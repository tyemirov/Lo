use crate::{context::*, value::*};
use num_traits::{Signed, ToPrimitive};
use std::{
    ops::{Deref, DerefMut},
    sync::Arc,
};

pub fn admit(site: &Site, flows: &[Flow]) -> Result<(), String> {
    let invalid = || format!("invalid native collection contract {}", site.operation);
    let operation = site.operation.as_str();
    let unfold = operation == "List.unfold";
    let fold = matches!(operation, "List.fold" | "Flow.fold");
    let context_start = if unfold || fold { 2 } else { 1 };
    if site.arguments.len() < context_start + 1 {
        return Err(invalid());
    }
    let Some(Argument::Flow(callback)) = site.arguments.last() else {
        return Err(invalid());
    };
    let flow = flows.get(*callback).ok_or_else(invalid)?;
    let inputs = site.arguments[..site.arguments.len() - 1]
        .iter()
        .map(|argument| match argument {
            Argument::Value(typ) => Ok(typ.clone()),
            Argument::Flow(_) => Err(invalid()),
        })
        .collect::<Result<Vec<_>, _>>()?;
    let mut parameters = inputs[context_start..].to_vec();
    if unfold || fold {
        parameters.push(inputs[if unfold { 0 } else { 1 }].clone());
    }
    if unfold {
        if inputs[1] != atom("Int") {
            return Err(invalid());
        }
    } else {
        let Type::List(element) = &inputs[0] else {
            return Err(invalid());
        };
        parameters.push(*element.clone());
    }
    if parameters.len() != flow.parameters.len()
        || parameters
            .iter()
            .zip(&flow.parameters)
            .any(|(typ, (_, expected))| typ != expected)
    {
        return Err(invalid());
    }
    let output = flow.output.base();
    let result = match operation {
        "List.map" => Type::List(Box::new(output)),
        "List.filter" => {
            if output != atom("Bool") {
                return Err(invalid());
            }
            inputs[0].clone()
        }
        "List.fold" | "Flow.fold" => {
            if output != inputs[1] {
                return Err(invalid());
            }
            output
        }
        "List.flat_map" => {
            if !matches!(output, Type::List(_)) {
                return Err(invalid());
            }
            output
        }
        "List.unfold" => {
            let Type::Record(fields) = &output else {
                return Err(invalid());
            };
            if fields.len() != 2
                || fields[0].0 != "next_state"
                || fields[0].1 != inputs[0]
                || fields[1].0 != "value"
            {
                return Err(invalid());
            }
            Type::List(Box::new(fields[1].1.clone()))
        }
        _ => return Err(invalid()),
    };
    let expected = if matches!(flow.output, Type::Fallible(_)) {
        Type::Fallible(Box::new(result))
    } else {
        result
    };
    if site.typ != expected {
        return Err(invalid());
    }
    Ok(())
}

struct Scope {
    context: *mut Context,
    id: usize,
    output: Option<Handle>,
}

impl Scope {
    unsafe fn enter(context: *mut Context) -> Self {
        (*context).scopes += 1;
        let id = (*context).resources.scope_enter();
        Self {
            context,
            id,
            output: None,
        }
    }
}

impl Drop for Scope {
    fn drop(&mut self) {
        unsafe {
            let roots =
                crate::tensor::allocations(&self.output.iter().cloned().collect::<Vec<_>>());
            (*self.context).resources.scope_close(self.id, &roots);
            (*self.context).scopes -= 1;
        }
    }
}

fn callback_failure(
    site: &Site,
    cause: &Handle,
    index: usize,
    element: Option<Handle>,
    state: Option<Handle>,
    progress: Option<&Handle>,
) -> Fault {
    let mut fields = vec![("index".into(), int(index))];
    if let Some(element) = element {
        fields.push(("element".into(), element));
    }
    if let Some(state) = state {
        fields.push(("state".into(), state));
    }
    let owner = &site.owner;
    let framed = frame(cause, owner, &site.operation, record(fields));
    let Data::Error(original) = &framed.data else {
        std::process::abort()
    };
    let mut error = original.clone();
    error.site = site.id;
    error.operation = site.operation.clone();
    if let Some(progress) = progress {
        error.flow_progress.insert(0, progress.clone());
    }
    Fault::Recoverable(new(atom("Error"), Data::Error(error)))
}

struct CallbackArguments<'a>(&'a mut Vec<Handle>);
impl Deref for CallbackArguments<'_> {
    type Target = Vec<Handle>;
    fn deref(&self) -> &Self::Target {
        self.0
    }
}
impl DerefMut for CallbackArguments<'_> {
    fn deref_mut(&mut self) -> &mut Self::Target {
        self.0
    }
}
impl Drop for CallbackArguments<'_> {
    fn drop(&mut self) {
        self.0.clear();
    }
}

/// The context pointer permits callback reentry without retaining a Rust borrow.
pub unsafe fn execute(context: *mut Context, site: &Site, args: &[Handle]) -> Outcome {
    let mut scope = Scope::enter(context);
    let operation = site.operation.as_str();
    let unfold = operation == "List.unfold";
    let fold = matches!(operation, "List.fold" | "Flow.fold");
    let flow_fold = operation == "Flow.fold";
    let mut state = if unfold {
        Some(args[0].clone())
    } else if fold {
        Some(args[1].clone())
    } else {
        None
    };
    let count = if unfold {
        let count = args[1].integer();
        if count.is_positive() {
            count
                .to_usize()
                .filter(|count| *count <= MAX_ITEMS)
                .ok_or_else(|| limit("unfold elements"))?
        } else {
            0
        }
    } else {
        args[0].items().len()
    };
    let context_start = if fold || unfold { 2 } else { 1 };
    let parameters = &args[context_start..args.len() - 1];
    let Data::Flow(flow) = args.last().unwrap().data else {
        return Err(host("missing callback identity"));
    };
    let mut progress = if flow_fold {
        Some((*context).record_flow_progress(count, 0, state.as_ref().unwrap())?)
    } else {
        None
    };
    let mut result = (|| -> Outcome {
        let mut output = Vec::new();
        let mut callback_storage = Vec::new();
        for index in 0..count {
            (*context).step()?;
            let mut callback_args = CallbackArguments(&mut callback_storage);
            callback_args.extend_from_slice(parameters);
            if let Some(state) = &state {
                callback_args.push(state.clone());
            }
            let element = if unfold {
                None
            } else {
                Some(args[0].items()[index].clone())
            };
            if let Some(element) = &element {
                callback_args.push(element.clone());
            }
            let callback_value = crate::exports::invoke(context, flow, &callback_args, site.id)?;
            let value = match &callback_value.data {
                Data::Success(value) => value.clone(),
                Data::Failure(cause) => {
                    return Err(callback_failure(
                        site,
                        cause,
                        index,
                        element,
                        state.clone(),
                        progress.as_ref(),
                    ));
                }
                _ => callback_value,
            };
            match operation {
                "List.map" => output.push(value),
                "List.filter" => {
                    if value.bool() {
                        output.push(element.unwrap());
                    }
                }
                "List.fold" | "Flow.fold" => state = Some(value),
                "List.flat_map" => {
                    let extra = value.items();
                    if extra.len() > MAX_ITEMS - output.len() {
                        return Err(limit("list elements"));
                    }
                    output.extend_from_slice(extra);
                }
                "List.unfold" => {
                    let fields = value.fields();
                    output.push(fields["value"].clone());
                    (*context).resources.scope_retain(
                        scope.id,
                        &crate::tensor::allocations(&[fields["value"].clone()]),
                    );
                    state = Some(fields["next_state"].clone());
                }
                _ => return Err(host("invalid collection processor")),
            }
            if fold || unfold {
                (*context).resources.scope_keep(
                    scope.id,
                    &crate::tensor::allocations(&[state.as_ref().unwrap().clone()]),
                );
            }
            if flow_fold {
                progress = Some((*context).record_flow_progress(
                    count,
                    index + 1,
                    state.as_ref().unwrap(),
                )?);
            }
        }
        if fold {
            Ok(state.unwrap())
        } else {
            Ok(new(site.typ.base(), Data::List(output)))
        }
    })();
    if result
        .as_ref()
        .err()
        .is_some_and(|fault| !matches!(fault.primary(), Fault::Recoverable(_)))
    {
        if let Some(progress) = progress {
            if !(*context).flow_fault_progress {
                (*context).flow_progress = vec![progress];
            } else if !(*context)
                .flow_progress
                .first()
                .is_some_and(|first| Arc::ptr_eq(first, &progress))
            {
                (*context).flow_progress.insert(0, progress);
            }
            (*context).flow_fault_progress = true;
            result = result.map_err(|fault| {
                fault.with_details(crate::context::BoundaryDetails {
                    flow_progress: (*context).flow_progress.clone(),
                    ..Default::default()
                })
            });
        }
    }
    if let Ok(output) = &result {
        scope.output = Some(output.clone());
    }
    result
}
