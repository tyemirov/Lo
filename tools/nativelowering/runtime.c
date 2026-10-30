/* Bounded I006 test runtime. This is not the production value implementation. */
typedef unsigned long long u64;
typedef long long i64;
typedef unsigned int u32;
typedef struct lo_context lo_context;
typedef struct lo_value lo_value;
typedef struct { u32 status, code; const lo_value *value; } lo_result;
typedef void (*lo_callback)(lo_context *, const lo_value *const *, u64, u64, lo_result *);
extern int printf(const char *, ...);
extern unsigned long strlen(const char *);
extern int memcmp(const void *, const void *, unsigned long);
extern void *memcpy(void *, const void *, unsigned long);
extern void abort(void);
_Static_assert(sizeof(lo_result) == 16, "P005 result size");
_Static_assert(__builtin_offsetof(lo_result, value) == 8, "P005 result layout");

enum { K_INT, K_BOOL, K_STRING, K_NULL, K_LIST, K_FALLIBLE, K_ERROR, K_FLOW, K_RECORD };
enum { LIMIT = 1, CANCELLED = 2, UNSUPPORTED = 3, DEPTH = 4, INVALID_STATUS = 5 };
enum { MAX_VALUES = 1024, MAX_ITEMS = 64, MAX_TOKENS = 32768, MAX_FRAMES = 16 };
typedef struct { int start, end, parent, children; char kind; } token;
struct lo_value {
    lo_context *owner;
    u64 references, weight, count, frame_count, flow;
    int kind, failed;
    i64 integer;
    char text[256], operation[64], frames[MAX_FRAMES][64], labels[MAX_ITEMS][32];
    const lo_value *items[MAX_ITEMS];
};
struct lo_context {
    const char *metadata;
    int metadata_bytes, token_count, types, locations, flows;
    token tokens[MAX_TOKENS];
    const lo_callback *callbacks;
    u64 callback_count, remaining, steps, weight, weight_limit, live, used, scopes, depth;
    i64 cancel_after;
    int first_event, invalid_result;
    lo_value values[MAX_VALUES];
};
static lo_context context;
#define ABI(name) void name(lo_context *c, const lo_value *const *a, u64 n, u64 site, lo_result *out)
static void require(int condition) { if (!condition) abort(); }
static int equal(const char *a, const char *b) { return strlen(a) == strlen(b) && !memcmp(a, b, strlen(a)); }
static void text_copy(char *out, u64 capacity, const char *in) {
    u64 size = strlen(in); require(size < capacity); memcpy(out, in, size + 1);
}
static void json_string(const char *text) {
    printf("\"");
    for (const unsigned char *p = (const unsigned char *)text; *p; p++) {
        if (*p == '\\' || *p == '"') printf("\\%c", *p);
        else if (*p < 32) printf("\\u%04x", (unsigned)*p);
        else printf("%c", *p);
    }
    printf("\"");
}
static void event_start(lo_context *c) { if (!c->first_event) printf(","); c->first_event = 0; }
static void event(lo_context *c, const char *name) { event_start(c); json_string(name); }
static void event_number(lo_context *c, const char *name, i64 number) {
    event_start(c); printf("\"%s:%lld\"", name, number);
}
static void event_text(lo_context *c, const char *prefix, const char *text) {
    event_start(c); printf("\"%s:%s\"", prefix, text);
}
static void result(lo_result *out, u32 status, u32 code, const lo_value *value) {
    out->status = status; out->code = code; out->value = value;
}
static void terminal(lo_context *c, lo_result *out, u32 code) {
    event(c, code == LIMIT ? "limit" : code == CANCELLED ? "cancel" : code == INVALID_STATUS ? "invalid-status" : "unsupported");
    result(out, 2, code, 0);
}
static lo_value *value_new(lo_context *c, int kind, u64 weight) {
    require(c->used < MAX_VALUES);
    lo_value *v = &c->values[c->used++];
    v->owner = c; v->references = 1; v->kind = kind; v->weight = weight; c->live++;
    return v;
}
void lo_value_retain(const lo_value *input) {
    if (!input) return;
    lo_value *v = (lo_value *)input; require(v->references != 0); v->references++;
}
void lo_value_release(const lo_value *input) {
    if (!input) return;
    lo_value *v = (lo_value *)input; require(v->references != 0);
    if (--v->references) return;
    v->owner->live--;
    for (u64 i = 0; i < v->count; i++) lo_value_release(v->items[i]);
}
static lo_value *integer(lo_context *c, i64 number) {
    u64 magnitude = number < 0 ? (u64)(-(number + 1)) + 1 : (u64)number, bytes = 0;
    for (u64 n = magnitude; n; n >>= 8) bytes++;
    lo_value *v = value_new(c, K_INT, 16 + bytes); v->integer = number; return v;
}
static lo_value *boolean(lo_context *c, int flag) {
    lo_value *v = value_new(c, K_BOOL, 17); v->integer = !!flag; return v;
}
static lo_value *string(lo_context *c, const char *text) {
    lo_value *v = value_new(c, K_STRING, 16 + strlen(text)); text_copy(v->text, sizeof v->text, text); return v;
}
static lo_value *container(lo_context *c, int kind, const lo_value *const *items, u64 count) {
    require(count <= MAX_ITEMS); lo_value *v = value_new(c, kind, 16); v->count = count;
    for (u64 i = 0; i < count; i++) { require(items[i] != 0); v->items[i] = items[i]; lo_value_retain(items[i]); v->weight += items[i]->weight; }
    return v;
}
static lo_value *wrap(lo_context *c, const lo_value *input, int failed) {
    lo_value *v = container(c, K_FALLIBLE, &input, 1); v->failed = failed; return v;
}

static void whitespace(lo_context *c, int *offset) {
    while (*offset < c->metadata_bytes && (c->metadata[*offset] == ' ' || c->metadata[*offset] == '\n' || c->metadata[*offset] == '\r' || c->metadata[*offset] == '\t')) (*offset)++;
}
static int parse_json(lo_context *c, int *offset, int parent) {
    whitespace(c, offset); if (*offset >= c->metadata_bytes || c->token_count == MAX_TOKENS) return -1;
    int index = c->token_count++; token *t = &c->tokens[index]; t->start = *offset; t->parent = parent;
    char first = c->metadata[*offset]; t->kind = first;
    if (first == '{' || first == '[') {
        char closing = first == '{' ? '}' : ']'; (*offset)++; whitespace(c, offset);
        while (*offset < c->metadata_bytes && c->metadata[*offset] != closing) {
            if (parse_json(c, offset, index) < 0) return -1; t->children++;
            whitespace(c, offset);
            if (first == '{') {
                if (*offset >= c->metadata_bytes || c->metadata[(*offset)++] != ':') return -1;
                if (parse_json(c, offset, index) < 0) return -1; t->children++; whitespace(c, offset);
            }
            if (*offset < c->metadata_bytes && c->metadata[*offset] == ',') { (*offset)++; whitespace(c, offset); }
            else break;
        }
        if (*offset >= c->metadata_bytes || c->metadata[(*offset)++] != closing) return -1;
    } else if (first == '"') {
        t->start = ++*offset;
        while (*offset < c->metadata_bytes && c->metadata[*offset] != '"') {
            if (c->metadata[*offset] == '\\') (*offset)++;
            if (*offset >= c->metadata_bytes) return -1; (*offset)++;
        }
        if (*offset == c->metadata_bytes) return -1; t->end = (*offset)++; return index;
    } else {
        while (*offset < c->metadata_bytes && c->metadata[*offset] != ',' && c->metadata[*offset] != '}' && c->metadata[*offset] != ']' && c->metadata[*offset] > ' ') (*offset)++;
        if (*offset == t->start) return -1;
    }
    t->end = *offset; return index;
}
static int token_equal(lo_context *c, int index, const char *text) {
    return index >= 0 && c->tokens[index].end - c->tokens[index].start == (int)strlen(text) && !memcmp(c->metadata + c->tokens[index].start, text, strlen(text));
}
static int next_token(lo_context *c, int index) {
    int end = c->tokens[index].end;
    while (++index < c->token_count && c->tokens[index].start < end) {}
    return index;
}
static int field(lo_context *c, int object, const char *name) {
    if (object < 0 || c->tokens[object].kind != '{') return -1;
    for (int key = object + 1; key < c->token_count && c->tokens[key].start < c->tokens[object].end;) {
        int value = key + 1; if (token_equal(c, key, name)) return value; key = next_token(c, value);
    }
    return -1;
}
static int array_at(lo_context *c, int array, u64 index) {
    require(array >= 0 && c->tokens[array].kind == '[' && index < (u64)c->tokens[array].children);
    int token_index = array + 1;
    while (index--) token_index = next_token(c, token_index);
    return token_index;
}
static i64 decimal(const char *text, int *ok) {
    int negative = *text == '-'; if (negative) text++;
    u64 magnitude = 0, ceiling = negative ? 9223372036854775808ULL : 9223372036854775807ULL;
    if (!*text) { *ok = 0; return 0; }
    for (; *text; text++) {
        unsigned digit = (unsigned)(*text - '0');
        if (digit > 9 || magnitude > (ceiling - digit) / 10) { *ok = 0; return 0; }
        magnitude = magnitude * 10 + digit;
    }
    return negative ? (magnitude == 9223372036854775808ULL ? (-9223372036854775807LL - 1) : -(i64)magnitude) : (i64)magnitude;
}
static void token_text(lo_context *c, int index, char *out, int capacity) {
    require(index >= 0); token t = c->tokens[index]; int size = 0;
    for (int i = t.start; i < t.end; i++) {
        unsigned char ch = (unsigned char)c->metadata[i];
        if (ch == '\\') {
            require(++i < t.end); ch = (unsigned char)c->metadata[i];
            if (ch == 'n') ch = '\n'; else if (ch == 'r') ch = '\r'; else if (ch == 't') ch = '\t';
            else if (ch == 'u') {
                unsigned point = 0;
                for (int j = 0; j < 4; j++) {
                    require(++i < t.end); unsigned h = (unsigned char)c->metadata[i];
                    unsigned digit = h >= '0' && h <= '9' ? h - '0' : h >= 'a' && h <= 'f' ? h - 'a' + 10 : h >= 'A' && h <= 'F' ? h - 'A' + 10 : 99;
                    require(digit < 16); point = point * 16 + digit;
                }
                require(point < 128); ch = (unsigned char)point;
            }
        }
        require(size + 1 < capacity); out[size++] = (char)ch;
    }
    out[size] = 0;
}
static u64 token_number(lo_context *c, int index) {
    char text[32]; token_text(c, index, text, sizeof text); int ok = 1; i64 n = decimal(text, &ok); require(ok && n >= 0); return (u64)n;
}
static int location(lo_context *c, u64 id) { return array_at(c, c->locations, id); }
static int type_record(lo_context *c, u64 site) {
    u64 id = token_number(c, field(c, location(c, site), "type")); return array_at(c, c->types, id);
}
static void type_kind(lo_context *c, u64 site, char *out, int bytes) { token_text(c, field(c, type_record(c, site), "kind"), out, bytes); }
static void site_text(lo_context *c, u64 site, const char *name, char *out, int bytes) { int row = location(c, site); int object = equal(name,"text") || equal(name,"field") ? field(c,row,"payload") : row; token_text(c, field(c, object, name), out, bytes); }

ABI(lo_step) {
    event(c, "step");
    if (c->cancel_after >= 0 && c->steps >= (u64)c->cancel_after) { terminal(c, out, CANCELLED); return; }
    if (!c->remaining) { terminal(c, out, LIMIT); return; }
    c->remaining--; c->steps++; result(out, 0, 0, 0);
}
ABI(lo_charge_value) {
    require(n == 1 && a[0]); event_number(c, "charge", (i64)a[0]->weight);
    if (a[0]->weight > 16777216ULL || a[0]->weight > c->weight_limit - c->weight) { terminal(c, out, LIMIT); return; }
    c->weight += a[0]->weight; result(out, 0, 0, 0);
}
ABI(lo_literal) {
    if (c->invalid_result) { event(c, "invalid-result"); result(out, 99, 0, (const lo_value *)1); return; }
    char kind[32], text[256]; type_kind(c, site, kind, sizeof kind); site_text(c, site, "text", text, sizeof text);
    lo_value *v;
    if (equal(kind, "Int")) { int ok = 1; i64 number = decimal(text, &ok); if (!ok) { terminal(c, out, UNSUPPORTED); return; } v = integer(c, number); event_number(c, "literal", number); }
    else if (equal(kind, "Bool")) { v = boolean(c, equal(text, "true")); event_text(c, "literal", text); }
    else if (equal(kind, "String")) { v = string(c, text); event_text(c, "literal", text); }
    else if (equal(kind, "Null")) { v = value_new(c, K_NULL, 16); event(c, "literal:null"); }
    else { terminal(c, out, UNSUPPORTED); return; }
    result(out, 0, 0, v);
}
ABI(lo_scope_enter) { c->scopes++; result(out, 0, 0, 0); }
void lo_scope_leave(lo_context *c, const lo_value *escaping, u64 site) { (void)escaping; (void)site; require(c->scopes != 0); c->scopes--; }
ABI(lo_flow_enter) { if (c->depth == 256) { terminal(c, out, DEPTH); return; } c->depth++; result(out, 0, 0, 0); }
void lo_flow_leave(lo_context *c) { require(c->depth != 0); c->depth--; }
ABI(lo_flow_ref) { lo_value *v = value_new(c, K_FLOW, 0); v->flow = token_number(c, field(c, location(c, site), "flow")); require(v->flow < c->callback_count); result(out, 0, 0, v); }
ABI(lo_invalid_status) { terminal(c, out, INVALID_STATUS); }
u32 lo_boolean_value(const lo_value *v) { require(v && v->references && v->kind == K_BOOL); return (u32)v->integer; }
ABI(lo_match) {
    require(n == 2 && a[0] && a[1]); int matched = 0;
    if (a[0]->kind == a[1]->kind) {
        if (a[0]->kind == K_INT || a[0]->kind == K_BOOL) matched = a[0]->integer == a[1]->integer;
        else if (a[0]->kind == K_STRING) matched = equal(a[0]->text, a[1]->text);
        else if (a[0]->kind == K_NULL) matched = 1;
        else { terminal(c, out, UNSUPPORTED); return; }
    }
    event(c, matched ? "match:true" : "match:false"); result(out, 0, 0, boolean(c, matched));
}
ABI(lo_convert_success) { require(n == 1); result(out, 0, 0, wrap(c, a[0], 0)); }
ABI(lo_convert_failure) { require(n == 1 && a[0]->kind == K_ERROR); result(out, 0, 0, wrap(c, a[0], 1)); }
ABI(lo_convert_propagate) {
    require(n == 1 && a[0]->kind == K_FALLIBLE);
    int returning = token_equal(c, field(c, location(c, site), "action"), "cause");
    event(c, returning ? (a[0]->failed ? "flow-return:error" : "flow-return:success") : (a[0]->failed ? "propagate:error" : "propagate:success"));
    lo_value_retain(a[0]->items[0]); result(out, a[0]->failed ? 1 : 0, a[0]->failed ? 17 : 0, a[0]->items[0]);
}
ABI(lo_cause) {
    require(n == 1);
    if (a[0]->kind == K_FALLIBLE && a[0]->failed) { lo_value_retain(a[0]->items[0]); result(out, 1, 17, a[0]->items[0]); }
    else { lo_value_retain(a[0]); result(out, 0, 0, a[0]); }
}
ABI(lo_convert_record_view) { require(n == 1 && a[0]->kind == K_RECORD); lo_value_retain(a[0]); result(out, 0, 0, a[0]); }
ABI(lo_convert_fallible_record_view) { require(n == 1 && a[0]->kind == K_FALLIBLE); lo_value_retain(a[0]); result(out, 0, 0, a[0]); }
ABI(lo_convert_scalar_list_view) { terminal(c, out, UNSUPPORTED); }
ABI(lo_project) {
    require(n == 1); char name[64]; site_text(c, site, "field", name, sizeof name); event_text(c, "project", name);
    if (a[0]->kind == K_ERROR && equal(name, "kind")) { result(out, 0, 0, string(c, a[0]->text)); return; }
    if (a[0]->kind == K_RECORD) for (u64 i = 0; i < a[0]->count; i++) if (equal(name, a[0]->labels[i])) { lo_value_retain(a[0]->items[i]); result(out, 0, 0, a[0]->items[i]); return; }
    terminal(c, out, UNSUPPORTED);
}
static lo_value *frame_error(lo_context *c, const lo_value *error, const char *name, u64 input_weight) {
    require(error->kind == K_ERROR && error->frame_count < MAX_FRAMES);
    lo_value *v = value_new(c, K_ERROR, 0); *v = *error; v->references = 1; v->owner = c;
    text_copy(v->frames[v->frame_count++], sizeof v->frames[0], name);
    v->weight += 66 + strlen(name) + strlen(error->operation) + input_weight;
    event_text(c, "frame", name); return v;
}
void lo_flow_error(lo_context *c, u64 flow, const lo_value *const *a, u64 n, u64 site, lo_result *out) {
    (void)site; require(n >= 1 && a[0]->kind == K_ERROR);
    int metadata = array_at(c, c->flows, flow), parameters = field(c, metadata, "parameters");
    char name[64]; token_text(c, field(c, metadata, "name"), name, sizeof name);
    u64 input_weight = 16;
    for (u64 i = 0; i < (u64)c->tokens[parameters].children; i++) {
        int p = array_at(c, parameters, i); char parameter[64], type_name[128]; token_text(c, field(c, p, "name"), parameter, sizeof parameter);
        int type = array_at(c, c->types, token_number(c, field(c, p, "type"))); token_text(c, field(c, type, "name"), type_name, sizeof type_name);
        input_weight += strlen(parameter) + 36 + strlen(type_name);
    }
    lo_value *framed = frame_error(c, a[0], name, input_weight);
    if (token_equal(c, field(c, metadata, "handler"), "null")) text_copy(framed->operation, sizeof framed->operation, name);
    result(out, 1, 17, framed);
}

static void arithmetic(lo_context *c, const lo_value *const *a, u64 n, const char *name, lo_result *out) {
    require(n == 2 && a[0]->kind == K_INT && a[1]->kind == K_INT); event_text(c, "op", name);
    i64 left = a[0]->integer, right = a[1]->integer, number = 0; int overflow = 0;
    if (equal(name, "Math.add")) overflow = __builtin_add_overflow(left, right, &number);
    else if (equal(name, "Math.sub")) overflow = __builtin_sub_overflow(left, right, &number);
    else if (equal(name, "Math.mul")) overflow = __builtin_mul_overflow(left, right, &number);
    else if (equal(name, "Math.div")) {
        if (!right) {
            lo_value *error = value_new(c, K_ERROR, 16 + 17 + 16 + 16 + 21 + a[0]->weight + a[1]->weight + 16);
            text_copy(error->text, sizeof error->text, "Math.DivideByZero"); text_copy(error->operation, sizeof error->operation, name); result(out, 1, 17, error); return;
        }
        if (left == (-9223372036854775807LL - 1) && right == -1) overflow = 1; else number = left / right;
    } else { terminal(c, out, UNSUPPORTED); return; }
    if (overflow) { terminal(c, out, UNSUPPORTED); return; }
    result(out, 0, 0, integer(c, number));
}
ABI(lo_op_Math_2Eadd) { arithmetic(c, a, n, "Math.add", out); }
ABI(lo_op_Math_2Esub) { arithmetic(c, a, n, "Math.sub", out); }
ABI(lo_op_Math_2Emul) { arithmetic(c, a, n, "Math.mul", out); }
ABI(lo_op_Math_2Ediv) { arithmetic(c, a, n, "Math.div", out); }
ABI(lo_op_Math_2Eeq) { require(n == 2 && a[0]->kind == K_INT && a[1]->kind == K_INT); event(c, "op:Math.eq"); result(out, 0, 0, boolean(c, a[0]->integer == a[1]->integer)); }
ABI(lo_op_List_2Enew) { event(c, "op:List.new"); result(out, 0, 0, container(c, K_LIST, a, n)); }
ABI(lo_op_List_2Elength) { require(n == 1 && a[0]->kind == K_LIST); event(c, "op:List.length"); result(out, 0, 0, integer(c, (i64)a[0]->count)); }
ABI(lo_op_Record_2Enew) {
    event(c, "op:Record.new"); lo_value *v = container(c, K_RECORD, a, n); int labels = field(c, field(c, location(c, site), "payload"), "labels");
    require((u64)c->tokens[labels].children == n);
    for (u64 i = 0; i < n; i++) { token_text(c, array_at(c, labels, i), v->labels[i], sizeof v->labels[i]); v->weight += strlen(v->labels[i]); }
    result(out, 0, 0, v);
}
ABI(lo_op_Error_2Enew) {
    require(n == 3 && a[0]->kind == K_STRING && a[1]->kind == K_STRING && a[2]->kind == K_RECORD); event(c, "op:Error.new");
    lo_value *v = value_new(c, K_ERROR, 32 + strlen(a[0]->text) + strlen(a[1]->text) + a[2]->weight);
    text_copy(v->text, sizeof v->text, a[0]->text); text_copy(v->operation, sizeof v->operation, "Error.new"); result(out, 0, 0, v);
}
static int progress(lo_context *c, const lo_value *state, u64 total, u64 completed, lo_result *out) {
    u64 total_bytes = 0, completed_bytes = 0;
    for (u64 x = total; x; x >>= 8) total_bytes++;
    for (u64 x = completed; x; x >>= 8) completed_bytes++;
    u64 weight = 91 + state->weight + 16 + total_bytes + 2 * (16 + completed_bytes);
    event_number(c, "progress", (i64)weight);
    if (c->cancel_after >= 0 && c->steps >= (u64)c->cancel_after) { terminal(c, out, CANCELLED); return 0; }
    if (weight > 16777216 || weight > c->weight_limit - c->weight) { terminal(c, out, LIMIT); return 0; }
    c->weight += weight; return 1;
}
static void collection(lo_context *c, const lo_value *const *a, u64 n, u64 site, int fold, lo_result *out) {
    require(n >= (fold ? 3 : 2) && a[0]->kind == K_LIST && a[n - 1]->kind == K_FLOW);
    event(c, fold ? "op:Flow.fold" : "op:List.map");
    const lo_value *values[MAX_ITEMS], *state = fold ? a[1] : 0; u64 count = 0;
    if (state) lo_value_retain(state);
    c->scopes++;
    if (fold && !progress(c, state, a[0]->count, 0, out)) goto failed;
    for (u64 i = 0; i < a[0]->count; i++) {
        lo_result step; lo_step(c, 0, 0, site, &step);
        if (step.status) { *out = step; goto failed; }
        const lo_value *parameters[MAX_ITEMS]; u64 parameter_count = 0;
        for (u64 j = fold ? 2 : 1; j + 1 < n; j++) parameters[parameter_count++] = a[j];
        if (fold) parameters[parameter_count++] = state;
        parameters[parameter_count++] = a[0]->items[i];
        event_number(c, "callback", (i64)i);
        lo_result called; c->callbacks[a[n - 1]->flow](c, parameters, parameter_count, site, &called);
        if (called.status == 2) { *out = called; goto failed; }
        const lo_value *v = called.value; int failure = called.status == 1;
        if (!failure && v->kind == K_FALLIBLE) {
            const lo_value *base = v->items[0]; lo_value_retain(base); failure = v->failed; lo_value_release(v); v = base;
        }
        if (failure) {
                char owner[64]; site_text(c, site, "owner", owner, sizeof owner);
                lo_value *index = integer(c, (i64)i);
                u64 input_weight = 16 + 5 + index->weight + 7 + a[0]->items[i]->weight;
                if (fold) input_weight += 5 + state->weight;
                lo_value_release(index);
                lo_value *framed = frame_error(c, v, owner, input_weight);
                /* This frame records the collection operation, not the failing child operation. */
                framed->weight += strlen(fold ? "Flow.fold" : "List.map") - strlen(v->operation);
                text_copy(framed->operation, sizeof framed->operation, fold ? "Flow.fold" : "List.map");
                lo_value_release(v); result(out, 1, 17, framed); goto failed;
        }
        if (fold) { lo_value_release(state); state = v; if (!progress(c, state, a[0]->count, i + 1, out)) goto failed; }
        else { require(count < MAX_ITEMS); values[count++] = v; }
    }
    if (fold) { result(out, 0, 0, state); state = 0; }
    else { result(out, 0, 0, container(c, K_LIST, values, count)); }
    for (u64 i = 0; i < count; i++) lo_value_release(values[i]);
    c->scopes--; return;
failed:
    for (u64 i = 0; i < count; i++) lo_value_release(values[i]);
    lo_value_release(state); c->scopes--;
}
ABI(lo_op_List_2Emap) { collection(c, a, n, site, 0, out); }
ABI(lo_op_Flow_2Efold) { collection(c, a, n, site, 1, out); }
ABI(lo_commit) { terminal(c, out, UNSUPPORTED); }

void lo_program_admit(lo_context *c, const unsigned char *metadata, u64 bytes, const lo_callback *callbacks, u64 count, lo_result *out) {
    if (bytes > 1048576 || count > MAX_ITEMS) { terminal(c, out, UNSUPPORTED); return; }
    c->metadata = (const char *)metadata; c->metadata_bytes = (int)bytes; c->callbacks = callbacks; c->callback_count = count;
    int offset = 0;
    if (parse_json(c, &offset, -1) != 0) { terminal(c, out, UNSUPPORTED); return; }
    whitespace(c, &offset);
    if (offset != c->metadata_bytes || !token_equal(c, field(c, 0, "abi"), "lo-native-3")) { terminal(c, out, UNSUPPORTED); return; }
    c->types = field(c, 0, "types"); c->locations = field(c, 0, "locations"); c->flows = field(c, 0, "flows");
    require(c->types >= 0 && c->locations >= 0 && c->flows >= 0 && (u64)c->tokens[c->flows].children == count);
    int files = field(c,0,"files"), spans = field(c,0,"spans");
    require(files>=0 && spans>=0 && c->tokens[files].kind=='[' && c->tokens[spans].kind=='[');
    for (u64 i=0;i<(u64)c->tokens[c->locations].children;i++) require(token_number(c,field(c,location(c,i),"span"))<(u64)c->tokens[spans].children);
    result(out, 0, 0, 0);
}
static void print_value(const lo_value *v) {
    if (!v) { printf("null"); return; }
    require(v->references != 0);
    switch (v->kind) {
    case K_INT: printf("%lld", v->integer); break;
    case K_BOOL: printf("%s", v->integer ? "true" : "false"); break;
    case K_STRING: json_string(v->text); break;
    case K_NULL: printf("null"); break;
    case K_LIST:
        printf("["); for (u64 i = 0; i < v->count; i++) { if (i) printf(","); print_value(v->items[i]); } printf("]"); break;
    case K_FALLIBLE: printf(v->failed ? "{\"failure\":" : "{\"success\":"); print_value(v->items[0]); printf("}"); break;
    case K_ERROR:
        printf("{\"kind\":"); json_string(v->text); printf(",\"frames\":[");
        for (u64 i = 0; i < v->frame_count; i++) { if (i) printf(","); json_string(v->frames[i]); } printf("]}"); break;
    case K_FLOW: printf("{\"flow\":%llu}", v->flow); break;
    case K_RECORD:
        printf("{"); for (u64 i = 0; i < v->count; i++) { if (i) printf(","); json_string(v->labels[i]); printf(":"); print_value(v->items[i]); } printf("}"); break;
    default: abort();
    }
}
extern const unsigned char lo_program_metadata[];
extern const u64 lo_program_metadata_size, lo_program_flow_count;
extern const lo_callback lo_program_flows[];
extern ABI(lo_entry);
int main(int argc, char **argv) {
    if (argc != 4 && argc != 5) return 64;
    int ok = 1; i64 budget = decimal(argv[1], &ok), cancel_after = decimal(argv[2], &ok), weight_limit = decimal(argv[3], &ok);
    if (!ok || budget < 0 || cancel_after < -1 || weight_limit < 0 || weight_limit > 67108864) return 64;
    lo_context *c = &context; c->remaining = (u64)budget; c->cancel_after = cancel_after; c->first_event = 1; c->weight_limit = (u64)weight_limit; c->invalid_result = argc == 5 && equal(argv[4], "invalid-result");
    printf("{\"events\":["); lo_result out;
    lo_program_admit(c, lo_program_metadata, lo_program_metadata_size, lo_program_flows, lo_program_flow_count, &out);
    if (out.status == 0) lo_entry(c, 0, 0, 0, &out);
    require(out.status <= 2 && (out.status != 2 || out.value == 0));
    printf("],\"status\":%u,\"code\":%u,\"value\":", out.status, out.code); print_value(out.value);
    lo_value_release(out.value);
    printf(",\"steps\":%llu,\"weight\":%llu,\"live\":%llu,\"scopes\":%llu,\"depth\":%llu}\n", c->steps, c->weight, c->live, c->scopes, c->depth);
    return 0;
}
