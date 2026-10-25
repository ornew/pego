# G020: Lazy Backend Preparation

A compiled grammar need not prepare every execution backend before its first
parse. PEGO delays derived bytecode, VM metadata and the recognition graph until
an operation needs them, then caches the prepared state.

## Mechanism and Invariants

Program owns distinct `sync.Once` guards for the module, recursive VM, iterative
VM and recognizer. `Module` compiles bytecode once; `rule` prepares the selected
VM's metadata from it. `recognizer` builds a separate graph and caches either
its program or its setup error. Grammar-less programs cannot construct closure
or recognition graphs. The ordinary closure parse graph is built eagerly during
compilation.

## Scope and State

Default engine setup for closure recognition and both VMs. Generated Go
Node/typed Go and TypeScript embed their machinery and are outside scope. This
is first-use preparation, not per-rule matching work.

## Reference Control

No same-source eager-preparation control or private opt-in alternative is
maintained.

## Evidence and Implementation

There is no numbered optimization record or isolated timing/allocation
measurement. Warm parse results do not quantify avoided setup, and first-use
timing must separate preparation from steady-state matching.

Source owners: [`Program.moduleOnce, recOnce, vmOnce and
ivmOnce`](../../../internal/engine/compile.go),
[`Program.Module`](../../../internal/engine/bcompile.go), [`Program.recognizer
and rule`](../../../internal/engine/parse.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [E040](E040-recognition-lowering.md), [M070](M070-vm-predecoded-match-data.md).

[Optimization Catalog](../README.md)
