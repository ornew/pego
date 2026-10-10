# 14. One operand stack for VM expression code

- The bytecode backends evaluate actions and predicates with stack-based expression code (`vmProgram.eval`). Each
  evaluation started with a nil `[]any` and grew it by appending, `ENew` copied the field values and `ECall` the
  arguments into fresh slices, `ENew` built its field-name list each time, every lambda call (`vmFunc.apply`) built
  a new locals slice, and every Pratt operator action a 3-element one for `$lhs`, `$rhs`, `$op`. On JSON these
  accounted for about half of the VM's allocations, and were the whole difference from the closure engine.
- Operands now live on one stack per parser (`parser.estack`). An evaluation works above the current top and
  returns the stack to its height. `ENew` and `ECall` pass the top of the stack to `newStruct` and the built-ins,
  which do not retain it; during a built-in call the arguments stay on the stack and lambdas called by it push
  above them. Lambda and operator locals are pushed onto the same stack. Field-name lists are resolved once when
  the program is loaded.
- `EFUNC` copies the locals when there are any, because a function can outlive the lambda that created it (a lambda
  may return it) while its locals are popped. Lambdas in rule actions have no locals and do not copy.
- Effect (min of 6 interleaved runs, codepoints, Apple M3 Max; allocs/op and time before → after):

  | Workload | bytecode | iterative | closure (unchanged) |
  |:--|:--|:--|:--|
  | JSON | 292k → 147k; 28.5 → 27.2 ms | 292k → 147k; 40.0 → 39.0 ms | 153k |
  | CSV | 201k → 116k; 13.7 → 12.8 ms | 201k → 116k; 16.5 → 15.8 ms | 131k |
  | XML | 165k → 72k; 25.9 → 24.3 ms | 165k → 72k; 35.9 → 34.5 ms | 64k |
  | Arith_Pratt | 231k → 94k; 20.2 → 17.6 ms | 231k → 95k; 29.0 → 26.6 ms | 134k |
  | Minilang | 140k → 68k; 26.3 → 24.8 ms | 140k → 68k; 33.6 → 32.4 ms | 75k |

  The VMs now allocate less often than the closure engine on JSON, CSV, Pratt and minilang.
