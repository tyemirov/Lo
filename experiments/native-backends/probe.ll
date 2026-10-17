define i64 @lo_probe(i64 %x, ptr %status) {
entry:
  %invalid = icmp slt i64 %x, 0
  br i1 %invalid, label %error, label %success
error:
  store i32 7, ptr %status
  ret i64 0
success:
  store i32 0, ptr %status
  %scaled = mul i64 %x, 3
  %value = add i64 %scaled, 2
  ret i64 %value
}
