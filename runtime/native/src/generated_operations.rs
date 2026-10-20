use crate::{
    context::Context,
    exports::{call, ResultSlot},
    value::Value,
};
#[no_mangle]
pub unsafe extern "C" fn lo_op_Int_2Enew(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Int.new"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_String_2Enew(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("String.new"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Bool_2Enew(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Bool.new"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Float_2Enew(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Float.new"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Scalar_2Enew(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Scalar.new"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_File_2EPath_2Enew(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("File.Path.new"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Bytes_2Enew(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Bytes.new"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_UTF8_2EBytes_2Enew(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("UTF8.Bytes.new"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_UTF8_2EString_2Enew(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("UTF8.String.new"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_String_2EFromScalars_2Enew(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("String.FromScalars.new"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_List_2Enew(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("List.new"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Record_2Enew(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Record.new"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Map_2Enew(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Map.new"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Range_2Enew(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Range.new"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_String_2Econcat(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("String.concat"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_String_2Eupper(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("String.upper"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_String_2Elower(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("String.lower"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_String_2Elength(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("String.length"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_String_2Eeq(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("String.eq"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Scalar_2Eeq(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Scalar.eq"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_File_2EPath_2Eeq(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("File.Path.eq"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Int_2Eto_5Fstring(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Int.to_string"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Int_2Eto_5Ffloat(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Int.to_float"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Float_2Eto_5Fint(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Float.to_int"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Float_2Eto_5Fstring(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Float.to_string"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Float_2Eadd(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Float.add"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Float_2Esub(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Float.sub"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Float_2Emul(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Float.mul"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Float_2Ediv(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Float.div"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Float_2Esqrt(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Float.sqrt"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Float_2Eeq(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Float.eq"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Float_2Elt(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Float.lt"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Float_2Elte(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Float.lte"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Float_2Egt(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Float.gt"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Float_2Egte(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Float.gte"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Math_2Eadd(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Math.add"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Math_2Esub(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Math.sub"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Math_2Emul(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Math.mul"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Math_2Ediv(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Math.div"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Math_2Emod(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Math.mod"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Math_2Elt(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Math.lt"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Math_2Elte(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Math.lte"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Math_2Egt(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Math.gt"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Math_2Egte(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Math.gte"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Math_2Eeq(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Math.eq"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Math_2Esum(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Math.sum"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Math_2Eproduct(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Math.product"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Math_2Emin(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Math.min"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Math_2Emax(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Math.max"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_List_2Elength(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("List.length"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Range_2Econtains(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Range.contains"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Range_2Eis_5Fempty(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Range.is_empty"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Record_2Emerge(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Record.merge"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Map_2Ehas_5Fkey(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Map.has_key"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Map_2Eget(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Map.get"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Error_2Enew(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Error.new"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_List_2Emap(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("List.map"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_List_2Efilter(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("List.filter"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_List_2Efold(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("List.fold"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_List_2Eflat_5Fmap(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("List.flat_map"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_List_2Eunfold(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("List.unfold"))
}
#[no_mangle]
pub unsafe extern "C" fn lo_op_Flow_2Efold(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Flow.fold"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Device_2Enew(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Device.new"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Tensor_2Enew(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Tensor.new"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Tensor_2Eto(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Tensor.to"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Tensor_2Eshape(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Tensor.shape"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Tensor_2Erank(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Tensor.rank"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Tensor_2Edtype(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Tensor.dtype"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Tensor_2Edevice(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Tensor.device"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Tensor_2Eto_5Ffloat(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Tensor.to_float"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Tensor_2Evalues(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Tensor.values"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Tensor_2Eadd(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Tensor.add"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Tensor_2Esub(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Tensor.sub"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Tensor_2Emul(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Tensor.mul"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Tensor_2Ediv(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Tensor.div"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Tensor_2Esqrt(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Tensor.sqrt"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Tensor_2Erelu(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Tensor.relu"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Tensor_2Ematmul(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Tensor.matmul"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Tensor_2Ebroadcast(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Tensor.broadcast"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Tensor_2Ereshape(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Tensor.reshape"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Tensor_2Etranspose(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Tensor.transpose"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Tensor_2Esum(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Tensor.sum"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Tensor_2Emean(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Tensor.mean"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Parameters_2Enew(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Parameters.new"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Parameters_2Evalues(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Parameters.values"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Gradients_2Enew(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Gradients.new"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Gradients_2Evalues(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Gradients.values"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Gradients_2Ezero(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Gradients.zero"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Gradients_2Eadd(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Gradients.add"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Gradients_2Escale(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Gradients.scale"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Optimizer_2ESGD_2EConfig_2Enew(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Optimizer.SGD.Config.new"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Optimizer_2EAdamW_2EConfig_2Enew(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Optimizer.AdamW.Config.new"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Optimizer_2ESGD_2EState_2Enew(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Optimizer.SGD.State.new"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Optimizer_2EAdamW_2EState_2Enew(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Optimizer.AdamW.State.new"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Optimizer_2ESGD_2Estep(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Optimizer.SGD.step"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Optimizer_2EAdamW_2Estep(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Optimizer.AdamW.step"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Checkpoint_2Eencode(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Checkpoint.encode"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Checkpoint_2Edecode(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Checkpoint.decode"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Parameters_2Eto(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Parameters.to"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Optimizer_2ESGD_2EState_2Eto(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Optimizer.SGD.State.to"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Optimizer_2EAdamW_2EState_2Eto(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Optimizer.AdamW.State.to"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_File_2Eread(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("File.read"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_File_2EWrite_2Enew(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("File.Write.new"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_derivative_enter(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("lo_derivative_enter"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_derivative_snapshot(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("lo_derivative_snapshot"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_derivative_retain(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("lo_derivative_retain"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_derivative_loss(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("lo_derivative_loss"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_derivative_zero(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("lo_derivative_zero"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_derivative_seed(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("lo_derivative_seed"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_derivative_reverse(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("lo_derivative_reverse"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_derivative_finish(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("lo_derivative_finish"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_derivative_failure(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("lo_derivative_failure"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_op_Autodiff_2Estop(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("Autodiff.stop"))
}

#[no_mangle]
pub unsafe extern "C" fn lo_derivative_host_failure(
    c: *mut Context,
    a: *const *const Value,
    n: u64,
    s: u64,
    o: *mut ResultSlot,
) {
    call(c, a, n, s, o, Some("lo_derivative_host_failure"))
}
