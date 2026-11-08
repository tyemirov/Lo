# Operation Reference

[Documentation](../README.md) · [Philosophy](../philosophy.md) · [Syntax](../syntax.md)

This reference describes all 165 implemented operations in the built-in catalog.
Each operation has an individual entry with a signature, behavior, failures, authority requirements, example, and detailed contract.
Namespace names group related operations.
They are not import statements or user-defined modules.

## Read A Signature

`T`, `U`, `K`, `V`, `S`, and `R` denote types in signature notation.
They are not source aliases or user-defined generic declarations.
`context...` denotes explicit callback context parameters, and `...` denotes operation-specific variable arguments.
A named callback is a flow reference rather than a first-class function value.

`T!` identifies a recoverable result.
Callback effects and output types can determine an operation's actual fallibility.
Each entry states its additional constraints.
The compiler checks calls against the concrete argument and callback types.

Construction, host observation, and activation are distinct.
An inert target can have construction failures and separate activation failures.
Host cancellation, budget exhaustion, and unresolved cleanup remain terminal where the host contract requires them.
An empty error list in a constructor does not establish that its later commit succeeds.

## By Subject

| Subject | Operations |
| --- | --- |
| [Values And Arithmetic](values.md) | 32 |
| [Text, Bytes, And Globs](text.md) | 30 |
| [Collections And Sequential Flows](collections.md) | 20 |
| [Files](files.md) | 15 |
| [Processes](process.md) | 12 |
| [HTTP](http.md) | 9 |
| [Applications](applications.md) | 6 |
| [Tensors And Devices](tensors.md) | 21 |
| [Training And Checkpoints](training.md) | 20 |

## Alphabetical Index

| Operation | Reference |
| --- | --- |
| [`Autodiff.grad`](training.md#autodiffgrad) | [Training And Checkpoints](training.md) |
| [`Autodiff.stop`](training.md#autodiffstop) | [Training And Checkpoints](training.md) |
| [`Bool.new`](values.md#boolnew) | [Values And Arithmetic](values.md) |
| [`Bytes.length`](text.md#byteslength) | [Text, Bytes, And Globs](text.md) |
| [`Bytes.new`](text.md#bytesnew) | [Text, Bytes, And Globs](text.md) |
| [`Bytes.slice`](text.md#bytesslice) | [Text, Bytes, And Globs](text.md) |
| [`Bytes.split`](text.md#bytessplit) | [Text, Bytes, And Globs](text.md) |
| [`Bytes.starts_with`](text.md#bytesstarts_with) | [Text, Bytes, And Globs](text.md) |
| [`Bytes.trim_start`](text.md#bytestrim_start) | [Text, Bytes, And Globs](text.md) |
| [`CLI.parse`](applications.md#cliparse) | [Applications](applications.md) |
| [`Checkpoint.decode`](training.md#checkpointdecode) | [Training And Checkpoints](training.md) |
| [`Checkpoint.encode`](training.md#checkpointencode) | [Training And Checkpoints](training.md) |
| [`Console.Line.new`](applications.md#consolelinenew) | [Applications](applications.md) |
| [`Device.new`](tensors.md#devicenew) | [Tensors And Devices](tensors.md) |
| [`Error.new`](values.md#errornew) | [Values And Arithmetic](values.md) |
| [`File.Append.new`](files.md#fileappendnew) | [Files](files.md) |
| [`File.Directory.child`](files.md#filedirectorychild) | [Files](files.md) |
| [`File.Directory.entry`](files.md#filedirectoryentry) | [Files](files.md) |
| [`File.Directory.refresh`](files.md#filedirectoryrefresh) | [Files](files.md) |
| [`File.Directory.scan`](files.md#filedirectoryscan) | [Files](files.md) |
| [`File.Entry.observe`](files.md#fileentryobserve) | [Files](files.md) |
| [`File.Path.eq`](files.md#filepatheq) | [Files](files.md) |
| [`File.Path.join`](files.md#filepathjoin) | [Files](files.md) |
| [`File.Path.name`](files.md#filepathname) | [Files](files.md) |
| [`File.Path.new`](files.md#filepathnew) | [Files](files.md) |
| [`File.Path.parent`](files.md#filepathparent) | [Files](files.md) |
| [`File.Path.to_string`](files.md#filepathto_string) | [Files](files.md) |
| [`File.Remove.checked`](files.md#fileremovechecked) | [Files](files.md) |
| [`File.Remove.new`](files.md#fileremovenew) | [Files](files.md) |
| [`File.Tree.Remove.checked`](files.md#filetreeremovechecked) | [Files](files.md) |
| [`File.Tree.snapshot`](files.md#filetreesnapshot) | [Files](files.md) |
| [`File.Write.new`](files.md#filewritenew) | [Files](files.md) |
| [`File.directory`](files.md#filedirectory) | [Files](files.md) |
| [`File.list`](files.md#filelist) | [Files](files.md) |
| [`File.open_paths`](files.md#fileopen_paths) | [Files](files.md) |
| [`File.read`](files.md#fileread) | [Files](files.md) |
| [`File.scan`](files.md#filescan) | [Files](files.md) |
| [`Float.add`](values.md#floatadd) | [Values And Arithmetic](values.md) |
| [`Float.div`](values.md#floatdiv) | [Values And Arithmetic](values.md) |
| [`Float.eq`](values.md#floateq) | [Values And Arithmetic](values.md) |
| [`Float.gt`](values.md#floatgt) | [Values And Arithmetic](values.md) |
| [`Float.gte`](values.md#floatgte) | [Values And Arithmetic](values.md) |
| [`Float.lt`](values.md#floatlt) | [Values And Arithmetic](values.md) |
| [`Float.lte`](values.md#floatlte) | [Values And Arithmetic](values.md) |
| [`Float.mul`](values.md#floatmul) | [Values And Arithmetic](values.md) |
| [`Float.new`](values.md#floatnew) | [Values And Arithmetic](values.md) |
| [`Float.sqrt`](values.md#floatsqrt) | [Values And Arithmetic](values.md) |
| [`Float.sub`](values.md#floatsub) | [Values And Arithmetic](values.md) |
| [`Float.to_int`](values.md#floatto_int) | [Values And Arithmetic](values.md) |
| [`Float.to_string`](values.md#floatto_string) | [Values And Arithmetic](values.md) |
| [`Flow.fold`](collections.md#flowfold) | [Collections And Sequential Flows](collections.md) |
| [`Flow.worklist`](collections.md#flowworklist) | [Collections And Sequential Flows](collections.md) |
| [`Glob.matches`](text.md#globmatches) | [Text, Bytes, And Globs](text.md) |
| [`Glob.new`](text.md#globnew) | [Text, Bytes, And Globs](text.md) |
| [`Gradients.add`](training.md#gradientsadd) | [Training And Checkpoints](training.md) |
| [`Gradients.new`](training.md#gradientsnew) | [Training And Checkpoints](training.md) |
| [`Gradients.scale`](training.md#gradientsscale) | [Training And Checkpoints](training.md) |
| [`Gradients.values`](training.md#gradientsvalues) | [Training And Checkpoints](training.md) |
| [`Gradients.zero`](training.md#gradientszero) | [Training And Checkpoints](training.md) |
| [`HTTP.Client.Request.new`](http.md#httpclientrequestnew) | [HTTP](http.md) |
| [`HTTP.Headers.new`](http.md#httpheadersnew) | [HTTP](http.md) |
| [`HTTP.Headers.values`](http.md#httpheadersvalues) | [HTTP](http.md) |
| [`HTTP.Method.new`](http.md#httpmethodnew) | [HTTP](http.md) |
| [`HTTP.Response.Bytes.new`](http.md#httpresponsebytesnew) | [HTTP](http.md) |
| [`HTTP.Response.Text.new`](http.md#httpresponsetextnew) | [HTTP](http.md) |
| [`HTTP.Response.new`](http.md#httpresponsenew) | [HTTP](http.md) |
| [`HTTP.Server.new`](http.md#httpservernew) | [HTTP](http.md) |
| [`HTTP.URL.new`](http.md#httpurlnew) | [HTTP](http.md) |
| [`Host.Invocation.current`](applications.md#hostinvocationcurrent) | [Applications](applications.md) |
| [`Int.new`](values.md#intnew) | [Values And Arithmetic](values.md) |
| [`Int.to_float`](values.md#intto_float) | [Values And Arithmetic](values.md) |
| [`Int.to_string`](values.md#intto_string) | [Values And Arithmetic](values.md) |
| [`List.at`](collections.md#listat) | [Collections And Sequential Flows](collections.md) |
| [`List.filter`](collections.md#listfilter) | [Collections And Sequential Flows](collections.md) |
| [`List.flat_map`](collections.md#listflat_map) | [Collections And Sequential Flows](collections.md) |
| [`List.fold`](collections.md#listfold) | [Collections And Sequential Flows](collections.md) |
| [`List.length`](collections.md#listlength) | [Collections And Sequential Flows](collections.md) |
| [`List.map`](collections.md#listmap) | [Collections And Sequential Flows](collections.md) |
| [`List.new`](collections.md#listnew) | [Collections And Sequential Flows](collections.md) |
| [`List.slice`](collections.md#listslice) | [Collections And Sequential Flows](collections.md) |
| [`List.sort`](collections.md#listsort) | [Collections And Sequential Flows](collections.md) |
| [`List.unfold`](collections.md#listunfold) | [Collections And Sequential Flows](collections.md) |
| [`Lock.File.new`](applications.md#lockfilenew) | [Applications](applications.md) |
| [`Map.get`](collections.md#mapget) | [Collections And Sequential Flows](collections.md) |
| [`Map.has_key`](collections.md#maphas_key) | [Collections And Sequential Flows](collections.md) |
| [`Map.new`](collections.md#mapnew) | [Collections And Sequential Flows](collections.md) |
| [`Math.add`](values.md#mathadd) | [Values And Arithmetic](values.md) |
| [`Math.div`](values.md#mathdiv) | [Values And Arithmetic](values.md) |
| [`Math.eq`](values.md#matheq) | [Values And Arithmetic](values.md) |
| [`Math.gt`](values.md#mathgt) | [Values And Arithmetic](values.md) |
| [`Math.gte`](values.md#mathgte) | [Values And Arithmetic](values.md) |
| [`Math.lt`](values.md#mathlt) | [Values And Arithmetic](values.md) |
| [`Math.lte`](values.md#mathlte) | [Values And Arithmetic](values.md) |
| [`Math.max`](values.md#mathmax) | [Values And Arithmetic](values.md) |
| [`Math.min`](values.md#mathmin) | [Values And Arithmetic](values.md) |
| [`Math.mod`](values.md#mathmod) | [Values And Arithmetic](values.md) |
| [`Math.mul`](values.md#mathmul) | [Values And Arithmetic](values.md) |
| [`Math.product`](values.md#mathproduct) | [Values And Arithmetic](values.md) |
| [`Math.sub`](values.md#mathsub) | [Values And Arithmetic](values.md) |
| [`Math.sum`](values.md#mathsum) | [Values And Arithmetic](values.md) |
| [`Optimizer.AdamW.Config.new`](training.md#optimizeradamwconfignew) | [Training And Checkpoints](training.md) |
| [`Optimizer.AdamW.State.new`](training.md#optimizeradamwstatenew) | [Training And Checkpoints](training.md) |
| [`Optimizer.AdamW.State.to`](training.md#optimizeradamwstateto) | [Training And Checkpoints](training.md) |
| [`Optimizer.AdamW.step`](training.md#optimizeradamwstep) | [Training And Checkpoints](training.md) |
| [`Optimizer.SGD.Config.new`](training.md#optimizersgdconfignew) | [Training And Checkpoints](training.md) |
| [`Optimizer.SGD.State.new`](training.md#optimizersgdstatenew) | [Training And Checkpoints](training.md) |
| [`Optimizer.SGD.State.to`](training.md#optimizersgdstateto) | [Training And Checkpoints](training.md) |
| [`Optimizer.SGD.step`](training.md#optimizersgdstep) | [Training And Checkpoints](training.md) |
| [`Parameters.new`](training.md#parametersnew) | [Training And Checkpoints](training.md) |
| [`Parameters.to`](training.md#parametersto) | [Training And Checkpoints](training.md) |
| [`Parameters.values`](training.md#parametersvalues) | [Training And Checkpoints](training.md) |
| [`Process.Arguments.format_shell`](process.md#processargumentsformat_shell) | [Processes](process.md) |
| [`Process.Command.new`](process.md#processcommandnew) | [Processes](process.md) |
| [`Process.Command.with_output`](process.md#processcommandwith_output) | [Processes](process.md) |
| [`Process.Command.with_timeout`](process.md#processcommandwith_timeout) | [Processes](process.md) |
| [`Process.Context.current`](process.md#processcontextcurrent) | [Processes](process.md) |
| [`Process.Context.environment_value`](process.md#processcontextenvironment_value) | [Processes](process.md) |
| [`Process.Context.new`](process.md#processcontextnew) | [Processes](process.md) |
| [`Process.Context.override_environment`](process.md#processcontextoverride_environment) | [Processes](process.md) |
| [`Process.Context.with_directory`](process.md#processcontextwith_directory) | [Processes](process.md) |
| [`Process.Context.without_environment`](process.md#processcontextwithout_environment) | [Processes](process.md) |
| [`Process.Executable.new`](process.md#processexecutablenew) | [Processes](process.md) |
| [`Process.Executable.resolve`](process.md#processexecutableresolve) | [Processes](process.md) |
| [`Process.Termination.exit_code`](process.md#processterminationexit_code) | [Processes](process.md) |
| [`Process.Termination.signal`](process.md#processterminationsignal) | [Processes](process.md) |
| [`Program.Exit.new`](applications.md#programexitnew) | [Applications](applications.md) |
| [`Program.path`](applications.md#programpath) | [Applications](applications.md) |
| [`Range.contains`](collections.md#rangecontains) | [Collections And Sequential Flows](collections.md) |
| [`Range.is_empty`](collections.md#rangeis_empty) | [Collections And Sequential Flows](collections.md) |
| [`Range.new`](collections.md#rangenew) | [Collections And Sequential Flows](collections.md) |
| [`Record.merge`](collections.md#recordmerge) | [Collections And Sequential Flows](collections.md) |
| [`Record.new`](collections.md#recordnew) | [Collections And Sequential Flows](collections.md) |
| [`Scalar.eq`](text.md#scalareq) | [Text, Bytes, And Globs](text.md) |
| [`Scalar.new`](text.md#scalarnew) | [Text, Bytes, And Globs](text.md) |
| [`String.FromScalars.new`](text.md#stringfromscalarsnew) | [Text, Bytes, And Globs](text.md) |
| [`String.compare`](text.md#stringcompare) | [Text, Bytes, And Globs](text.md) |
| [`String.concat`](text.md#stringconcat) | [Text, Bytes, And Globs](text.md) |
| [`String.ends_with`](text.md#stringends_with) | [Text, Bytes, And Globs](text.md) |
| [`String.eq`](text.md#stringeq) | [Text, Bytes, And Globs](text.md) |
| [`String.is_whitespace`](text.md#stringis_whitespace) | [Text, Bytes, And Globs](text.md) |
| [`String.length`](text.md#stringlength) | [Text, Bytes, And Globs](text.md) |
| [`String.lines`](text.md#stringlines) | [Text, Bytes, And Globs](text.md) |
| [`String.lower`](text.md#stringlower) | [Text, Bytes, And Globs](text.md) |
| [`String.new`](text.md#stringnew) | [Text, Bytes, And Globs](text.md) |
| [`String.replace_all`](text.md#stringreplace_all) | [Text, Bytes, And Globs](text.md) |
| [`String.slice`](text.md#stringslice) | [Text, Bytes, And Globs](text.md) |
| [`String.split`](text.md#stringsplit) | [Text, Bytes, And Globs](text.md) |
| [`String.split_first`](text.md#stringsplit_first) | [Text, Bytes, And Globs](text.md) |
| [`String.split_whitespace`](text.md#stringsplit_whitespace) | [Text, Bytes, And Globs](text.md) |
| [`String.starts_with`](text.md#stringstarts_with) | [Text, Bytes, And Globs](text.md) |
| [`String.trim`](text.md#stringtrim) | [Text, Bytes, And Globs](text.md) |
| [`String.upper`](text.md#stringupper) | [Text, Bytes, And Globs](text.md) |
| [`Tensor.add`](tensors.md#tensoradd) | [Tensors And Devices](tensors.md) |
| [`Tensor.broadcast`](tensors.md#tensorbroadcast) | [Tensors And Devices](tensors.md) |
| [`Tensor.device`](tensors.md#tensordevice) | [Tensors And Devices](tensors.md) |
| [`Tensor.div`](tensors.md#tensordiv) | [Tensors And Devices](tensors.md) |
| [`Tensor.dtype`](tensors.md#tensordtype) | [Tensors And Devices](tensors.md) |
| [`Tensor.matmul`](tensors.md#tensormatmul) | [Tensors And Devices](tensors.md) |
| [`Tensor.mean`](tensors.md#tensormean) | [Tensors And Devices](tensors.md) |
| [`Tensor.mul`](tensors.md#tensormul) | [Tensors And Devices](tensors.md) |
| [`Tensor.new`](tensors.md#tensornew) | [Tensors And Devices](tensors.md) |
| [`Tensor.rank`](tensors.md#tensorrank) | [Tensors And Devices](tensors.md) |
| [`Tensor.relu`](tensors.md#tensorrelu) | [Tensors And Devices](tensors.md) |
| [`Tensor.reshape`](tensors.md#tensorreshape) | [Tensors And Devices](tensors.md) |
| [`Tensor.shape`](tensors.md#tensorshape) | [Tensors And Devices](tensors.md) |
| [`Tensor.sqrt`](tensors.md#tensorsqrt) | [Tensors And Devices](tensors.md) |
| [`Tensor.sub`](tensors.md#tensorsub) | [Tensors And Devices](tensors.md) |
| [`Tensor.sum`](tensors.md#tensorsum) | [Tensors And Devices](tensors.md) |
| [`Tensor.to`](tensors.md#tensorto) | [Tensors And Devices](tensors.md) |
| [`Tensor.to_float`](tensors.md#tensorto_float) | [Tensors And Devices](tensors.md) |
| [`Tensor.transpose`](tensors.md#tensortranspose) | [Tensors And Devices](tensors.md) |
| [`Tensor.values`](tensors.md#tensorvalues) | [Tensors And Devices](tensors.md) |
| [`UTF8.Bytes.new`](text.md#utf8bytesnew) | [Text, Bytes, And Globs](text.md) |
| [`UTF8.String.new`](text.md#utf8stringnew) | [Text, Bytes, And Globs](text.md) |

## Language Forms

[Match expressions](../syntax.md#match-expressions), [projections](../syntax.md#types-and-records), and [commit statements](../syntax.md#commit-statements) have language syntax.
They are not additional entries in `catalog.All()`.
The [language reference](../../specification.md) defines their rules.

## Execution And Host Integration

A complete example can type-check without the authority necessary to execute its host operations.
The [native adapter guide](../native-adapters.md) defines executable grants and platform restrictions.
The [embedding guide](../native-embedding.md) defines the retained C ABI and its narrower supported boundaries.

The [backend matrix](../../specification.md#backend-support) defines implemented software and device support.
Proposed operations, including recursive filesystem APIs, remain outside this index.
