# Design

How PEGO is built today, and the records of the decisions that shaped it. Read these pages to find your way in
`internal/engine/` and the runtimes; read the [specification](../../spec/README.md) for what an implementation must do.

| Page | Contents |
|:--|:--|
| [Architecture](architecture.md) | The stages from grammar source to a running or generated parser, the backends, Documents, streams, observability, and which package owns each mechanism |
| [Core and Flow IR](intermediate-representations.md) | The two internal representations of the generated route: nodes, effects, verifier invariants, passes, text forms and the targets that consume them |
| [Design records](records/) | One record per decision, with its alternatives, validation and status |

The pages above describe what the code does. A record explains why a decision was taken and may describe work that is
proposed or only partly implemented; its status says which.
