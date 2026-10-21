use crate::{
    context::*,
    resources::Class,
    tensor::{self, Tensor},
    value::*,
};
use num_traits::ToPrimitive;
use serde_json::Value as Json;
use std::collections::{BTreeMap, BTreeSet};

#[derive(Clone, PartialEq, Eq)]
pub struct Frame {
    invocation: usize,
    name: String,
    arguments: Vec<(String, usize)>,
}
#[derive(Clone)]
pub struct Edge {
    operand: usize,
    node: usize,
}
#[derive(Clone)]
pub struct Node {
    location: usize,
    operation: String,
    operands: usize,
    operand_nodes: Vec<usize>,
    reverse: bool,
    conversion: bool,
    frames: Vec<Frame>,
    edges: Vec<Edge>,
}
#[derive(Clone)]
pub struct Plan {
    pub output: Type,
    location: usize,
    result_node: usize,
    nodes: Vec<Node>,
    selections: Vec<(String, usize)>,
    arguments: Vec<Type>,
}
pub struct Tape {
    plan: usize,
    snapshots: BTreeMap<usize, Handle>,
    storage: BTreeSet<u64>,
    bytes: u64,
    adjoints: BTreeMap<usize, Tensor>,
}

fn index(value: &Json, field: &str) -> Result<usize, String> {
    value[field]
        .as_u64()
        .and_then(|n| usize::try_from(n).ok())
        .ok_or_else(|| format!("invalid derivative {field}"))
}
fn array<'a>(value: &'a Json, field: &str) -> Result<&'a [Json], String> {
    value[field]
        .as_array()
        .map(Vec::as_slice)
        .ok_or_else(|| format!("invalid derivative {field}"))
}
fn string(value: &Json, field: &str) -> Result<String, String> {
    value[field]
        .as_str()
        .map(str::to_owned)
        .ok_or_else(|| format!("invalid derivative {field}"))
}

pub fn parse(value: &Json, types: &[Type], sites: &[Site]) -> Result<Vec<Plan>, String> {
    let records = value.as_array().ok_or("absent derivative plans")?;
    let mut plans = Vec::new();
    for (id, record) in records.iter().enumerate() {
        if index(record, "id")? != id {
            return Err("noncanonical derivative plan identifier".into());
        }
        let output = types
            .get(index(record, "type")?)
            .ok_or("unknown derivative output type")?
            .clone();
        if !matches!(output, Type::Fallible(_)) {
            return Err("derivative output must be fallible".into());
        }
        let location = index(record, "location")?;
        if sites
            .get(location)
            .map(|site| site.raw["action"] == "derivative_call" && site.typ == output)
            != Some(true)
        {
            return Err("invalid derivative call location".into());
        }
        if index(&sites[location].raw, "derivative_plan")? != id {
            return Err("derivative call plan mismatch".into());
        }
        let mut arguments = Vec::new();
        for typ in array(record, "arguments")? {
            let typ = typ
                .as_u64()
                .and_then(|id| types.get(id as usize))
                .ok_or("invalid derivative argument type")?;
            arguments.push(typ.clone());
        }
        if arguments.is_empty() {
            return Err("empty derivative arguments".into());
        }
        let records = array(record, "nodes")?;
        if records.is_empty() || records.len() > 100_000 {
            return Err("invalid derivative node count".into());
        }
        let mut nodes = Vec::new();
        let mut helper_frames = BTreeMap::new();
        for (position, record) in records.iter().enumerate() {
            let id = index(record, "id")?;
            if id != position + 1 {
                return Err("noncanonical derivative node identifier".into());
            }
            let location = index(record, "location")?;
            if sites
                .get(location)
                .map(|site| site.raw["action"] == "derivative_node")
                != Some(true)
            {
                return Err("invalid derivative node location".into());
            }
            let operation = string(record, "operation")?;
            let operands = index(record, "operands")?;
            let mut operand_nodes = Vec::new();
            for operand in array(record, "operand_nodes")? {
                let operand = operand
                    .as_u64()
                    .and_then(|id| usize::try_from(id).ok())
                    .ok_or("invalid derivative operand node")?;
                if operand == 0 || operand >= id {
                    return Err("invalid derivative operand provenance".into());
                }
                operand_nodes.push(operand);
            }
            if operand_nodes.len() != operands {
                return Err("derivative operand count mismatch".into());
            }
            let reverse = record["reverse"]
                .as_bool()
                .ok_or("invalid derivative reverse flag")?;
            let conversion = record["conversion"]
                .as_bool()
                .ok_or("invalid derivative conversion")?;
            let mut frames = Vec::new();
            let mut invocations = BTreeSet::new();
            for frame in array(record, "frames")? {
                let invocation = index(frame, "invocation")?;
                let name = string(frame, "name")?;
                if invocation == 0
                    || invocation > 100_000
                    || name.is_empty()
                    || !invocations.insert(invocation)
                    || frames.len() == 256
                {
                    return Err("invalid derivative helper frame".into());
                }
                let mut arguments = Vec::new();
                let mut names = BTreeSet::new();
                for argument in array(frame, "arguments")? {
                    let name = string(argument, "name")?;
                    let node = index(argument, "node")?;
                    if name.is_empty() || !names.insert(name.clone()) || node == 0 || node >= id {
                        return Err("invalid derivative frame argument".into());
                    }
                    arguments.push((name, node));
                }
                let frame = Frame {
                    invocation,
                    name,
                    arguments,
                };
                if helper_frames
                    .get(&invocation)
                    .is_some_and(|existing| existing != &frame)
                {
                    return Err("inconsistent derivative helper frame".into());
                }
                helper_frames.insert(invocation, frame.clone());
                frames.push(frame);
            }
            let mut edges = Vec::new();
            let mut previous = None;
            for edge in array(record, "edges")? {
                let operand = index(edge, "operand")?;
                let node = index(edge, "node")?;
                if node == 0
                    || node >= id
                    || operand >= operands
                    || operand_nodes[operand] != node
                    || previous.is_some_and(|previous| operand <= previous)
                {
                    return Err("invalid derivative edge order".into());
                }
                previous = Some(operand);
                edges.push(Edge { operand, node });
            }
            if !edges.is_empty()
                && !conversion
                && !matches!(
                    operation.as_str(),
                    "Tensor.add"
                        | "Tensor.sub"
                        | "Tensor.mul"
                        | "Tensor.div"
                        | "Tensor.sqrt"
                        | "Tensor.relu"
                        | "Tensor.matmul"
                        | "Tensor.broadcast"
                        | "Tensor.reshape"
                        | "Tensor.transpose"
                        | "Tensor.sum"
                        | "Tensor.mean"
                )
            {
                return Err("invalid derivative reverse operation".into());
            }
            if !edges.is_empty() {
                let expected_operands = if conversion {
                    1
                } else {
                    match operation.as_str() {
                        "Tensor.sqrt" | "Tensor.relu" => 1,
                        _ => 2,
                    }
                };
                if operands != expected_operands || conversion && !operation.is_empty() {
                    return Err("invalid derivative primitive operands".into());
                }
                for edge in &edges {
                    let target: &Node = &nodes[edge.node - 1];
                    if sites[target.location].typ.base() != atom("Tensor<FP32>") {
                        return Err("derivative edge targets a nontensor node".into());
                    }
                }
                if sites[location].typ.base() != atom("Tensor<FP32>") {
                    return Err("derivative reverse result is not a tensor".into());
                }
                if !conversion {
                    let left: &Node = &nodes[operand_nodes[0] - 1];
                    if sites[left.location].typ != atom("Tensor<FP32>") {
                        return Err("derivative left operand is not a tensor".into());
                    }
                    if operands == 2 {
                        let right: &Node = &nodes[operand_nodes[1] - 1];
                        let expected = if matches!(
                            operation.as_str(),
                            "Tensor.add"
                                | "Tensor.sub"
                                | "Tensor.mul"
                                | "Tensor.div"
                                | "Tensor.matmul"
                        ) {
                            atom("Tensor<FP32>")
                        } else {
                            Type::List(Box::new(atom("Int")))
                        };
                        if sites[right.location].typ != expected {
                            return Err("derivative right operand contract mismatch".into());
                        }
                    }
                }
            }
            nodes.push(Node {
                location,
                operation,
                operands,
                operand_nodes,
                reverse,
                conversion,
                frames,
                edges,
            });
        }
        let result_node = index(record, "result_node")?;
        if result_node == 0
            || result_node > nodes.len()
            || sites[nodes[result_node - 1].location].typ != atom("Tensor<FP32>")
        {
            return Err("invalid derivative result node".into());
        }
        let mut selections = Vec::new();
        let mut keys = BTreeSet::new();
        let mut selected_nodes = BTreeSet::new();
        for selection in array(record, "selections")? {
            let key = string(selection, "key")?;
            let node = index(selection, "node")?;
            if key.is_empty()
                || !keys.insert(key.clone())
                || node == 0
                || node > nodes.len()
                || sites[nodes[node - 1].location].typ != atom("Tensor<FP32>")
                || !nodes[node - 1].edges.is_empty()
                || !selected_nodes.insert(node)
            {
                return Err("invalid derivative selection".into());
            }
            selections.push((key, node));
        }
        if selections.is_empty() {
            return Err("empty derivative selections".into());
        }
        let mut gradient_fields = selections
            .iter()
            .map(|(key, _)| (key.clone(), atom("Tensor<FP32>")))
            .collect::<Vec<_>>();
        gradient_fields.sort_by(|a, b| a.0.cmp(&b.0));
        let expected = Type::Fallible(Box::new(Type::Record(vec![
            ("gradients".into(), Type::Record(gradient_fields)),
            ("value".into(), atom("Tensor<FP32>")),
        ])));
        if output != expected {
            return Err("derivative output schema mismatch".into());
        }
        let mut zeros = BTreeSet::new();
        let mut reverse_sites = BTreeSet::new();
        let mut snapshots = BTreeSet::new();
        let mut seeds = 0;
        for site in sites
            .iter()
            .filter(|site| site.raw["derivative_plan"].as_u64() == Some(id as u64))
        {
            match site.raw["action"].as_str() {
                Some("derivative_zero") => {
                    if !zeros.insert(index(&site.raw, "derivative_node")?) {
                        return Err("duplicate derivative zero site".into());
                    }
                }
                Some("derivative_seed") => {
                    seeds += 1;
                    if index(&site.raw, "derivative_node")? != result_node {
                        return Err("derivative seed result mismatch".into());
                    }
                }
                Some("derivative_reverse") => {
                    if !reverse_sites.insert(index(&site.raw, "derivative_node")?) {
                        return Err("duplicate derivative reverse site".into());
                    }
                }
                Some("derivative_snapshot") => {
                    if !snapshots.insert(index(&site.raw, "derivative_frame")?) {
                        return Err("duplicate derivative snapshot site".into());
                    }
                }
                _ => (),
            }
        }
        let expected_reverse = nodes
            .iter()
            .enumerate()
            .filter(|(_, node)| node.reverse && !node.edges.is_empty())
            .map(|(index, _)| index + 1)
            .collect::<BTreeSet<_>>();
        if zeros != selected_nodes
            || seeds != 1
            || reverse_sites != expected_reverse
            || snapshots != helper_frames.keys().copied().collect()
        {
            return Err("derivative helper plan mismatch".into());
        }
        plans.push(Plan {
            output,
            location,
            result_node,
            nodes,
            selections,
            arguments,
        });
    }
    Ok(plans)
}

pub fn admit_site(site: &Site, plans: &[Plan]) -> Result<(), String> {
    let action = site.raw["action"]
        .as_str()
        .ok_or("invalid derivative action")?;
    let expected_operation = if action == "derivative_call" {
        "Autodiff.grad"
    } else {
        ""
    };
    if site.operation != expected_operation
        || !site.arguments.is_empty()
        || (action != "derivative_snapshot" && !site.labels.is_empty())
    {
        return Err("derivative action identity mismatch".into());
    }
    let plan = index(&site.raw, "derivative_plan")?;
    let plan = plans.get(plan).ok_or("unknown derivative plan")?;
    let node = index(&site.raw, "derivative_node")?;
    match action {
        "derivative_call" | "derivative_enter" | "derivative_loss" | "derivative_finish"
            if node == 0 =>
        {
            ()
        }
        "derivative_node"
            if node > 0
                && node <= plan.nodes.len()
                && plan.nodes[node - 1].location == site.id as usize =>
        {
            ()
        }
        "derivative_snapshot"
        | "derivative_retain"
        | "derivative_zero"
        | "derivative_seed"
        | "derivative_reverse"
        | "derivative_failure"
        | "derivative_host_failure"
            if node > 0 && node <= plan.nodes.len() =>
        {
            if action == "derivative_snapshot" {
                let invocation = index(&site.raw, "derivative_frame")?;
                if !plan.nodes[node - 1]
                    .frames
                    .iter()
                    .any(|frame| frame.invocation == invocation)
                {
                    return Err("unknown derivative snapshot frame".into());
                }
                let unique: BTreeSet<_> = site.labels.iter().collect();
                if unique.len() != site.labels.len() {
                    return Err("duplicate derivative snapshot argument".into());
                }
            }
        }
        _ => return Err("invalid derivative helper location".into()),
    }
    let expected = match action {
        "derivative_call" | "derivative_finish" | "derivative_failure" => Some(plan.output.clone()),
        "derivative_loss" => Some(atom("Tensor<FP32>")),
        "derivative_node" => None,
        _ => Some(atom("Null")),
    };
    if expected.is_some_and(|expected| expected != site.typ) {
        return Err("derivative helper output mismatch".into());
    }
    if action == "derivative_seed" && node != plan.result_node {
        return Err("derivative seed node mismatch".into());
    }
    if action == "derivative_reverse"
        && (!plan.nodes[node - 1].reverse || plan.nodes[node - 1].edges.is_empty())
    {
        return Err("invalid derivative reverse helper".into());
    }
    if action == "derivative_zero"
        && !plan
            .selections
            .iter()
            .any(|(_, selected)| *selected == node)
    {
        return Err("invalid derivative zero helper".into());
    }
    if action == "derivative_snapshot" {
        let invocation = index(&site.raw, "derivative_frame")?;
        let frame = plan.nodes[node - 1]
            .frames
            .iter()
            .find(|frame| frame.invocation == invocation)
            .unwrap();
        if !site
            .labels
            .iter()
            .zip(&frame.arguments)
            .all(|(label, (name, _))| label == name)
            || site.labels.len() != frame.arguments.len()
        {
            return Err("derivative snapshot arguments mismatch".into());
        }
    }
    Ok(())
}

pub fn leave(context: &mut Context) {
    let tape = context.tapes.pop().unwrap();
    context.resources.unclassify(
        &tape.storage.iter().copied().collect::<Vec<_>>(),
        Class::Tape,
    );
}

fn tensor_value(value: &Handle) -> Result<&Tensor, Fault> {
    match &value.data {
        Data::Success(value) => Ok(tensor::data(value)),
        Data::Tensor(value) => Ok(value),
        Data::Failure(error) => Err(Fault::Recoverable(error.clone())),
        _ => Err(host("invalid derivative tensor result")),
    }
}

fn argument_types(
    context: &Context,
    plan: &Plan,
    site: &Site,
    node: usize,
    action: &str,
) -> Vec<Type> {
    let tensor = atom("Tensor<FP32>");
    let node_type = |id: usize| {
        context.program.sites[plan.nodes[id - 1].location]
            .typ
            .clone()
    };
    match action {
        "derivative_enter" | "derivative_host_failure" => vec![],
        "derivative_snapshot" => {
            let invocation = site.raw["derivative_frame"].as_u64().unwrap() as usize;
            plan.nodes[node - 1]
                .frames
                .iter()
                .find(|frame| frame.invocation == invocation)
                .unwrap()
                .arguments
                .iter()
                .map(|(_, node)| node_type(*node))
                .collect()
        }
        "derivative_retain" => vec![node_type(node)],
        "derivative_zero" | "derivative_seed" | "derivative_finish" => vec![tensor],
        "derivative_loss" => std::iter::once(tensor)
            .chain(plan.arguments.iter().cloned())
            .collect(),
        "derivative_failure" => vec![atom("Error")],
        "derivative_reverse" => {
            let metadata = &plan.nodes[node - 1];
            let mut types = if metadata.conversion {
                vec![]
            } else {
                metadata
                    .operand_nodes
                    .iter()
                    .map(|node| node_type(*node))
                    .collect()
            };
            if metadata.operation == "Tensor.sqrt" {
                types.push(node_type(node));
            }
            types
        }
        _ => std::process::abort(),
    }
}

pub fn execute(context: &mut Context, site: &Site, args: &[Handle]) -> Outcome {
    let plan_id = index(&site.raw, "derivative_plan").map_err(host)?;
    let program = context.program.clone();
    let plan = &program.derivatives[plan_id];
    let node_id = index(&site.raw, "derivative_node").map_err(host)?;
    let action = site.raw["action"].as_str().unwrap();
    let expected = argument_types(context, &plan, site, node_id, action);
    if args.len() != expected.len()
        || args
            .iter()
            .zip(&expected)
            .any(|(value, typ)| value.typ != *typ)
    {
        return Err(host(
            "derivative helper operands disagree with admitted plan",
        ));
    }
    if action == "derivative_enter" {
        if !args.is_empty() {
            return Err(host("derivative entry arity"));
        }
        context.tapes.push(Tape {
            plan: plan_id,
            snapshots: BTreeMap::new(),
            storage: BTreeSet::new(),
            bytes: 0,
            adjoints: BTreeMap::new(),
        });
        return Ok(new(atom("Null"), Data::Null));
    }
    if context.tapes.last().map(|tape| tape.plan) != Some(plan_id) {
        return Err(host("derivative tape identity mismatch"));
    }
    let result = (|| -> Outcome {
        match action {
            "derivative_host_failure" => {
                let fault = context
                    .fault
                    .clone()
                    .ok_or_else(|| host("absent derivative terminal fault"))?;
                return Err(boundary_host_fault(fault));
            }
            "derivative_snapshot" => {
                if args.len() != site.labels.len() {
                    return Err(host("derivative snapshot arity"));
                }
                let invocation = index(&site.raw, "derivative_frame").map_err(host)?;
                if context
                    .tapes
                    .last()
                    .unwrap()
                    .snapshots
                    .contains_key(&invocation)
                {
                    return Err(host("derivative snapshot repeated"));
                }
                let value = record(site.labels.iter().cloned().zip(args.iter().map(snapshot)));
                context.charge(&value)?;
                context
                    .tapes
                    .last_mut()
                    .unwrap()
                    .snapshots
                    .insert(invocation, value);
            }
            "derivative_retain" => {
                if args.len() != 1 {
                    return Err(host("derivative retain arity"));
                }
                let value = match &args[0].data {
                    Data::Success(value) => value,
                    _ => &args[0],
                };
                if let Data::Tensor(tensor) = &value.data {
                    context.resources.validate(&tensor.storage)?;
                    let tape = context.tapes.last_mut().unwrap();
                    if !tape.storage.contains(&tensor.storage.id) {
                        let bytes = tensor.storage.reserved_bytes();
                        if bytes > (32 << 20) - tape.bytes {
                            return Err(limit("derivative tape bytes"));
                        }
                        context
                            .resources
                            .classify(&[tensor.storage.id], Class::Tape)?;
                        tape.storage.insert(tensor.storage.id);
                        tape.bytes += bytes;
                    }
                }
            }
            "derivative_loss" => {
                if args.is_empty() {
                    return Err(host("derivative loss arity"));
                }
                if !tensor::data(&args[0]).shape.dims.is_empty() {
                    let source = &context.program.sites[plan.location];
                    let failure = operation_failure(
                        source,
                        &args[1..],
                        "Autodiff.InvalidLoss",
                        "loss must return a rank-zero tensor",
                    );
                    return Err(Fault::Recoverable(cause(&failure).unwrap()));
                }
                return Ok(args[0].clone());
            }
            "derivative_zero" | "derivative_seed" => {
                if args.len() != 1 {
                    return Err(host("derivative constant arity"));
                }
                let input = tensor::data(&args[0]);
                let constant = tensor::constant(
                    context,
                    &input.shape,
                    &input.storage.location,
                    if action == "derivative_seed" {
                        1.0
                    } else {
                        0.0
                    },
                )?;
                context
                    .tapes
                    .last_mut()
                    .unwrap()
                    .adjoints
                    .insert(node_id, constant);
            }
            "derivative_reverse" => {
                let node = &plan.nodes[node_id - 1];
                let expected = if node.conversion {
                    0
                } else {
                    node.operands + usize::from(node.operation == "Tensor.sqrt")
                };
                if args.len() != expected {
                    return Err(host("derivative reverse arity"));
                }
                let Some(gradient) = context
                    .tapes
                    .last()
                    .unwrap()
                    .adjoints
                    .get(&node_id)
                    .cloned()
                else {
                    return Ok(new(atom("Null"), Data::Null));
                };
                for edge in &node.edges {
                    let mut contribution = if node.conversion {
                        gradient.clone()
                    } else {
                        let left = tensor::data(&args[0]);
                        let mut right = None;
                        let mut indices = Vec::new();
                        if node.operands > 1 {
                            match &args[1].data {
                                Data::Tensor(tensor) => right = Some(tensor),
                                Data::List(items) => {
                                    for item in items {
                                        indices.push(item.integer().to_i64().unwrap());
                                    }
                                }
                                _ => return Err(host("derivative operand kind")),
                            }
                        }
                        let result = if node.operation == "Tensor.sqrt" {
                            tensor_value(args.last().unwrap())?
                        } else {
                            left
                        };
                        crate::derivative::vjp(
                            context,
                            &node.operation,
                            edge.operand,
                            &gradient,
                            left,
                            right,
                            result,
                            &indices,
                        )?
                    };
                    let previous = context
                        .tapes
                        .last()
                        .unwrap()
                        .adjoints
                        .get(&edge.node)
                        .cloned();
                    if let Some(previous) = previous {
                        contribution = tensor::apply(
                            context,
                            "Tensor.add",
                            &previous,
                            Some(&contribution),
                            &[],
                        )?;
                    }
                    context
                        .tapes
                        .last_mut()
                        .unwrap()
                        .adjoints
                        .insert(edge.node, contribution);
                }
                context.tapes.last_mut().unwrap().adjoints.remove(&node_id);
            }
            "derivative_finish" => {
                if args.len() != 1 {
                    return Err(host("derivative finish arity"));
                }
                let tape = context.tapes.last().unwrap();
                let mut fields = Vec::new();
                for (key, node) in &plan.selections {
                    let gradient = tape
                        .adjoints
                        .get(node)
                        .ok_or_else(|| host("derivative result has no selected adjoint"))?
                        .clone();
                    fields.push((key.clone(), tensor::value(gradient)));
                }
                let gradients = record(fields);
                return Ok(record([
                    ("value".into(), args[0].clone()),
                    ("gradients".into(), gradients),
                ]));
            }
            "derivative_failure" => {
                if args.len() != 1 || args[0].typ != atom("Error") {
                    return Err(host("derivative failure arity"));
                }
                let mut error = args[0].clone();
                for helper in plan.nodes[node_id - 1].frames.iter().rev() {
                    let Data::Error(original) = &error.data else {
                        std::process::abort()
                    };
                    let input = context
                        .tapes
                        .last()
                        .unwrap()
                        .snapshots
                        .get(&helper.invocation)
                        .ok_or_else(|| host("absent derivative helper snapshot"))?
                        .clone();
                    error = frame(&error, &helper.name, &original.operation, input);
                }
                return Err(Fault::Recoverable(error));
            }
            _ => return Err(host("invalid derivative action")),
        }
        Ok(new(atom("Null"), Data::Null))
    })();
    match result {
        Err(Fault::Numeric(kind, message)) => {
            let node = &plan.nodes[node_id - 1];
            let mut source = context.program.sites[node.location].clone();
            source.operation = node.operation.clone();
            let input = record(
                args.iter()
                    .take(node.operands)
                    .enumerate()
                    .map(|(index, value)| (format!("argument{index}"), value.clone())),
            );
            Err(Fault::Recoverable(error(&source, kind, &message, input)))
        }
        result => result,
    }
}
