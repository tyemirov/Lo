source_filename = "probe.ll"

declare i64 @lo_runtime_adjust(i64, ptr)

define i64 @lo_probe(i64 %x, ptr %status) !dbg !4 {
entry:
  %invalid = icmp slt i64 %x, 0, !dbg !8
  br i1 %invalid, label %error, label %success, !dbg !8
error:
  store i32 7, ptr %status, !dbg !11
  ret i64 0, !dbg !11
success:
  %scaled = mul i64 %x, 3, !dbg !12
  %value = call i64 @lo_runtime_adjust(i64 %scaled, ptr %status), !dbg !13
  ret i64 %value, !dbg !13
}

!llvm.dbg.cu = !{!0}
!llvm.module.flags = !{!9, !10}
!0 = distinct !DICompileUnit(language: DW_LANG_C, file: !1, producer: "Lo LLVM probe", isOptimized: false, runtimeVersion: 0, emissionKind: FullDebug)
!1 = !DIFile(filename: "probe.ll", directory: "experiments/native-backends")
!2 = !DISubroutineType(types: !3)
!3 = !{!5, !5, !6}
!4 = distinct !DISubprogram(name: "lo_probe", scope: !1, file: !1, line: 5, type: !2, scopeLine: 5, spFlags: DISPFlagDefinition, unit: !0)
!5 = !DIBasicType(name: "long long", size: 64, encoding: DW_ATE_signed)
!6 = !DIDerivedType(tag: DW_TAG_pointer_type, baseType: !7, size: 64)
!7 = !DIBasicType(name: "int", size: 32, encoding: DW_ATE_signed)
!8 = !DILocation(line: 7, column: 1, scope: !4)
!9 = !{i32 2, !"Dwarf Version", i32 4}
!10 = !{i32 2, !"Debug Info Version", i32 3}
!11 = !DILocation(line: 10, column: 1, scope: !4)
!12 = !DILocation(line: 13, column: 1, scope: !4)
!13 = !DILocation(line: 14, column: 1, scope: !4)
