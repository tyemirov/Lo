#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Action {
    DerivativeCall,
    DerivativeNode,
    DerivativeEnter,
    DerivativeSnapshot,
    DerivativeRetain,
    DerivativeLoss,
    DerivativeZero,
    DerivativeSeed,
    DerivativeReverse,
    DerivativeFinish,
    DerivativeFailure,
    DerivativeHostFailure,
    Flow,
    Body,
    Parameter,
    StatementStep,
    ExpressionStep,
    ScopeEnter,
    FlowEnter,
    Literal,
    FlowRef,
    FlowCall,
    Operation,
    ChargeValue,
    Reference,
    Project,
    Cause,
    Commit,
    Match,
    Predicate,
    Case,
    MatchResult,
    ConvertSuccess,
    ConvertFailure,
    ConvertPropagate,
    ConvertRecordView,
    ConvertScalarListView,
    ConvertFallibleRecordView,
}
impl Action {
    pub fn is_derivative(&self) -> bool {
        matches!(
            self,
            Self::DerivativeCall
                | Self::DerivativeNode
                | Self::DerivativeEnter
                | Self::DerivativeSnapshot
                | Self::DerivativeRetain
                | Self::DerivativeLoss
                | Self::DerivativeZero
                | Self::DerivativeSeed
                | Self::DerivativeReverse
                | Self::DerivativeFinish
                | Self::DerivativeFailure
                | Self::DerivativeHostFailure
        )
    }
    pub fn as_str(self) -> &'static str {
        match self {
            Self::DerivativeCall => "derivative_call",
            Self::DerivativeNode => "derivative_node",
            Self::DerivativeEnter => "derivative_enter",
            Self::DerivativeSnapshot => "derivative_snapshot",
            Self::DerivativeRetain => "derivative_retain",
            Self::DerivativeLoss => "derivative_loss",
            Self::DerivativeZero => "derivative_zero",
            Self::DerivativeSeed => "derivative_seed",
            Self::DerivativeReverse => "derivative_reverse",
            Self::DerivativeFinish => "derivative_finish",
            Self::DerivativeFailure => "derivative_failure",
            Self::DerivativeHostFailure => "derivative_host_failure",
            Self::Flow => "flow",
            Self::Body => "body",
            Self::Parameter => "parameter",
            Self::StatementStep => "statement_step",
            Self::ExpressionStep => "expression_step",
            Self::ScopeEnter => "scope_enter",
            Self::FlowEnter => "flow_enter",
            Self::Literal => "literal",
            Self::FlowRef => "flow_ref",
            Self::FlowCall => "flow_call",
            Self::Operation => "operation",
            Self::ChargeValue => "charge_value",
            Self::Reference => "reference",
            Self::Project => "project",
            Self::Cause => "cause",
            Self::Commit => "commit",
            Self::Match => "match",
            Self::Predicate => "predicate",
            Self::Case => "case",
            Self::MatchResult => "match_result",
            Self::ConvertSuccess => "convert_success",
            Self::ConvertFailure => "convert_failure",
            Self::ConvertPropagate => "convert_propagate",
            Self::ConvertRecordView => "convert_record_view",
            Self::ConvertScalarListView => "convert_scalar_list_view",
            Self::ConvertFallibleRecordView => "convert_fallible_record_view",
        }
    }
    pub fn parse(s: &str) -> Result<Self, String> {
        Ok(match s {
            "derivative_call" => Self::DerivativeCall,
            "derivative_node" => Self::DerivativeNode,
            "derivative_enter" => Self::DerivativeEnter,
            "derivative_snapshot" => Self::DerivativeSnapshot,
            "derivative_retain" => Self::DerivativeRetain,
            "derivative_loss" => Self::DerivativeLoss,
            "derivative_zero" => Self::DerivativeZero,
            "derivative_seed" => Self::DerivativeSeed,
            "derivative_reverse" => Self::DerivativeReverse,
            "derivative_finish" => Self::DerivativeFinish,
            "derivative_failure" => Self::DerivativeFailure,
            "derivative_host_failure" => Self::DerivativeHostFailure,
            "flow" => Self::Flow,
            "body" => Self::Body,
            "parameter" => Self::Parameter,
            "statement_step" => Self::StatementStep,
            "expression_step" => Self::ExpressionStep,
            "scope_enter" => Self::ScopeEnter,
            "flow_enter" => Self::FlowEnter,
            "literal" => Self::Literal,
            "flow_ref" => Self::FlowRef,
            "flow_call" => Self::FlowCall,
            "operation" => Self::Operation,
            "charge_value" => Self::ChargeValue,
            "reference" => Self::Reference,
            "project" => Self::Project,
            "cause" => Self::Cause,
            "commit" => Self::Commit,
            "match" => Self::Match,
            "predicate" => Self::Predicate,
            "case" => Self::Case,
            "match_result" => Self::MatchResult,
            "convert_success" => Self::ConvertSuccess,
            "convert_failure" => Self::ConvertFailure,
            "convert_propagate" => Self::ConvertPropagate,
            "convert_record_view" => Self::ConvertRecordView,
            "convert_scalar_list_view" => Self::ConvertScalarListView,
            "convert_fallible_record_view" => Self::ConvertFallibleRecordView,
            _ => return Err(format!("unknown native action {s}")),
        })
    }
}
