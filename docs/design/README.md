# Design

How ZeroParse is built today, and the records of the decisions that shaped it. Read these pages to find your way in
`internal/engine/` and the runtimes; read the [specification](../../spec/README.md) for what an implementation must do.

| Page | Contents |
|:--|:--|
| [Architecture](architecture.md) | How a grammar becomes a parser: the stages and why they are separate, the backends, Documents, streams, observability and the invariants that cut across packages |
| [Core and Flow IR](intermediate-representations.md) | Why there are two internal representations, what each is for, and how optimization works on them, with a worked example |
| [Design records](records/) | One record per decision, with its alternatives, validation and status |

The pages above describe the ideas and invariants that the code does not show on its own; the code is the source of truth for details. A record explains why a decision was taken and may describe work that is
proposed or only partly implemented; its status says which.
