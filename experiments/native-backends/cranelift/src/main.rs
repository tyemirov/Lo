use cranelift_codegen::{ir::{types, AbiParam, InstBuilder, condcodes::IntCC}, isa, settings};
use cranelift_frontend::{FunctionBuilder, FunctionBuilderContext};
use cranelift_module::{default_libcall_names, Linkage, Module};
use cranelift_object::{ObjectBuilder, ObjectModule};
use cranelift_codegen::settings::Configurable;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let args: Vec<String> = std::env::args().collect();
    if args.len() != 3 { return Err("expected target and object path".into()); }
    let target = args[1].parse::<target_lexicon::Triple>().map_err(|e| e.to_string())?;
    let mut flags = settings::builder();
    flags.set("is_pic", "true")?;
    let isa = isa::lookup(target)?.finish(settings::Flags::new(flags))?;
    let mut module = ObjectModule::new(ObjectBuilder::new(isa, "lo-probe", default_libcall_names())?);
    let mut runtime_signature = module.make_signature();
    runtime_signature.params.push(AbiParam::new(types::I64));
    runtime_signature.params.push(AbiParam::new(types::I64));
    runtime_signature.returns.push(AbiParam::new(types::I64));
    let runtime_id = module.declare_function("lo_runtime_adjust", Linkage::Import, &runtime_signature)?;
    let mut ctx = module.make_context();
    ctx.func.signature.params.push(AbiParam::new(types::I64));
    ctx.func.signature.params.push(AbiParam::new(types::I64));
    ctx.func.signature.returns.push(AbiParam::new(types::I64));
    let id = module.declare_function("lo_probe", Linkage::Export, &ctx.func.signature)?;
    let runtime = module.declare_func_in_func(runtime_id, &mut ctx.func);
    let mut builder_ctx = FunctionBuilderContext::new();
    {
        let mut b = FunctionBuilder::new(&mut ctx.func, &mut builder_ctx);
        let entry = b.create_block();
        let error = b.create_block();
        let success = b.create_block();
        b.append_block_params_for_function_params(entry);
        b.switch_to_block(entry);
        let x = b.block_params(entry)[0];
        let status = b.block_params(entry)[1];
        let invalid = b.ins().icmp_imm(IntCC::SignedLessThan, x, 0);
        b.ins().brif(invalid, error, &[], success, &[]);
        b.switch_to_block(error);
        let code = b.ins().iconst(types::I32, 7);
        b.ins().store(cranelift_codegen::ir::MemFlags::new(), code, status, 0);
        let zero = b.ins().iconst(types::I64, 0);
        b.ins().return_(&[zero]);
        b.switch_to_block(success);
        let scaled = b.ins().imul_imm(x, 3);
        let call = b.ins().call(runtime, &[scaled, status]);
        let value = b.inst_results(call)[0];
        b.ins().return_(&[value]);
        b.seal_all_blocks();
        b.finalize();
    }
    module.define_function(id, &mut ctx)?;
    std::fs::write(&args[2], module.finish().emit()?)?;
    Ok(())
}
