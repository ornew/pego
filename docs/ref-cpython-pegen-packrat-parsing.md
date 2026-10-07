# Left-Recursion Detection and Handling in pegen

This reference note explains how CPython's pegen parser generator detects and handles left recursion. PEGO's handling of left recursion follows the same approach (see [development.md](development.md#runtime)).

## 1. Introduction

Parsing expression grammars (PEGs) are a formalism for describing grammars, and their simplicity and expressiveness have led many parser generators to adopt them. However, PEG implementations based on classic recursive-descent parsing cannot directly handle grammars with **left recursion**, such as `expr: expr '+' term`; they recurse infinitely.

CPython's pegen parser generator solves this problem with the **packrat parsing** algorithm and an extension of it that supports left recursion. The core of the approach is a careful use of **memoization**.

This note explains in detail, based on the source code (`pegen/parser_generator.py` and `pegen/parser.py`), how pegen **detects left recursion statically** and **resolves it dynamically** at run time.

## 2. Overview: a two-phase approach

pegen's support for left recursion is divided into two phases:

1. **Static analysis (at parser generation time):**
   The grammar file is analyzed to determine in advance which rules are directly or indirectly left-recursive. This information is embedded in the generated parser code as metadata. This is done by `pegen/parser_generator.py`.
2. **Dynamic resolution (at parse time):**
   When a rule marked as left-recursive by the static analysis is called, a special memoization strategy avoids infinite recursion while finding the longest match. This is implemented by decorators in `pegen/parser.py`.

## 3. Phase 1: Detecting left recursion by static analysis

The purpose of this phase is to identify every left-recursive rule in the grammar before the parser is generated. The central function is `compute_left_recursives`.

### Step 1: Building the dependency graph

First, the left-call relationships between rules are represented as a **directed graph**. An edge from rule A to rule B means that "parsing rule A may begin by parsing rule B".

The graph is built by `make_first_graph`:

```python
# pegen/parser_generator.py

def make_first_graph(rules: Dict[str, Rule]) -> Dict[str, AbstractSet[str]]:
    """Compute the graph of left-invocations.

    There's an edge from A to B if A may invoke B at its initial
    position.

    Note that this requires the nullable flags to have been computed.
    """
    initial_name_visitor = InitialNamesVisitor(rules)
    graph = {}
    vertices: Set[str] = set()
    for rulename, rhs in rules.items():
        graph[rulename] = names = initial_name_visitor.visit(rhs)
        vertices |= names
    for vertex in vertices:
        graph.setdefault(vertex, set())
    return graph
```

The key is `InitialNamesVisitor`. It walks the right-hand side of a rule and collects every rule name that can come first in a parse.

It must take into account cases such as `rule: opt_b c`, where a rule that can match the empty string (a *nullable* rule) comes first: if `opt_b` is nullable, `rule` can begin with `c`. Therefore, before the graph is built, `NullableVisitor` determines for every rule whether it is nullable (`compute_nullables`).

```python
# pegen/parser_generator.py

class NullableVisitor(GrammarVisitor):
    # ... (logic that decides whether a rule or item can match the empty string)

def compute_nullables(rules: Dict[str, Rule]) -> Set[Any]:
    nullable_visitor = NullableVisitor(rules)
    for rule in rules.values():
        nullable_visitor.visit(rule)
    return nullable_visitor.nullables

class InitialNamesVisitor(GrammarVisitor):
    def __init__(self, rules: Dict[str, Rule]) -> None:
        self.rules = rules
        self.nullables = compute_nullables(rules)

    def visit_Alt(self, alt: Alt) -> Set[Any]:
        names: Set[str] = set()
        for item in alt.items:
            names |= self.visit(item)
            # If the item is not nullable, no later item can come first
            if item not in self.nullables:
                break
        return names

    def visit_NameLeaf(self, node: NameLeaf) -> Set[Any]:
        return {node.value}
    # ...
```

### Step 2: Detecting strongly connected components (SCCs)

Once the dependency graph has been built, its **strongly connected components (SCCs)** are detected. An SCC is a subgraph in which every pair of nodes (rules) is mutually reachable.

**A cycle in this graph means left recursion.** Detecting SCCs is the standard way to find all such cycles.

- A cycle A → B → A means that A and B are indirectly left-recursive.
- A self-loop A → A means that A is directly left-recursive.

### Step 3: Identifying left-recursive rules and leaders

`compute_left_recursives` uses `sccutils` to detect the SCCs, and for each SCC decides whether it is left-recursive and selects a leader.

```python
# pegen/parser_generator.py

def compute_left_recursives(
    rules: Dict[str, Rule]
) -> Tuple[Dict[str, AbstractSet[str]], List[AbstractSet[str]]]:
    graph = make_first_graph(rules)
    sccs = list(sccutils.strongly_connected_components(graph.keys(), graph))
    for scc in sccs:
        # An SCC larger than 1 means an indirect left-recursive cycle
        if len(scc) > 1:
            for name in scc:
                rules[name].left_recursive = True
            # ... leader selection logic ...
            leader = min(leaders)  # pick any leader from the candidates
            rules[leader].leader = True
        else:
            # For an SCC of size 1, check for a self-loop
            name = min(scc)
            if name in graph[name]:
                rules[name].left_recursive = True
                rules[name].leader = True
    return graph, sccs
```

- **`rule.left_recursive = True`**: every rule in the SCC is flagged as left-recursive.
- **`rule.leader = True`**: one *leader* is selected from each SCC as the entry point for run-time handling. Ideally, the leader lies on every cycle in the SCC. This restricts the special memoization described below to calls of the leader, which improves efficiency.

When this phase completes, it is known which rules are left-recursive and which rule is the starting point for handling each cycle. This information is used when generating the parser's C or Python code.

## 4. Phase 2: Resolving left recursion with memoization

At parse time, the left-recursive rules identified in phase 1 need special handling. This is implemented by decorators in `pegen/parser.py`.

### Ordinary memoization: the `@memoize` decorator

First, consider the memoization used for ordinary, non-left-recursive rules.

```python
# pegen/parser.py

def memoize(method: F) -> F:
    method_name = method.__name__

    def memoize_wrapper(self: "Parser", *args: object) -> Any:
        mark = self._mark()
        key = mark, method_name, args
        # On a cache hit, return the result and reset the position
        if key in self._cache:
            tree, endmark = self._cache[key]
            self._reset(endmark)
            return tree

        # Otherwise, run the method and store its result in the cache
        tree = method(self, *args)
        endmark = self._mark()
        self._cache[key] = tree, endmark
        return tree

    return cast(F, memoize_wrapper)
```

- **Cache key**: (start position, rule name, arguments)
- **Behavior**:
  1. If the key is in the cache, return the stored result (the parse tree and the end position).
  2. Otherwise, run the method for the rule, store its result and end position in the cache, and return it.

### Memoization for left recursion: the `@memoize_left_rec` decorator

Rules marked as left-recursion leaders (`leader=True`) get a special decorator, `@memoize_left_rec`. This is the core of the left-recursion solution and implements the algorithm known as **growing the seed**.

```python
# pegen/parser.py

def memoize_left_rec(
    method: Callable[["Parser"], Optional[T]]
) -> Callable[["Parser"], Optional[T]]:
    method_name = method.__name__

    def memoize_left_rec_wrapper(self: "Parser") -> Optional[T]:
        mark = self._mark()
        key = mark, method_name, ()

        # Ordinary cache hit
        if key in self._cache:
            tree, endmark = self._cache[key]
            self._reset(endmark)
            return tree

        # =================================================================
        # The "growing the seed" algorithm
        # =================================================================

        # Step A: initialize the cache with "failure" (planting the seed).
        # This prevents recursive calls from looping forever.
        self._cache[key] = None, mark
        lastresult, lastmark = None, mark

        while True:
            # Step B: reset the position and try to parse again
            self._reset(mark)
            result = method(self)
            endmark = self._mark()

            # Step C: check for progress.
            # If there is no result or the position did not advance, growth ends.
            if not result or endmark <= lastmark:
                break

            # On progress, update the cache with the better result (growing the seed)
            self._cache[key] = lastresult, lastmark = result, endmark

        # Step D: return the longest match and reset the position
        self._reset(lastmark)
        tree = lastresult

        # Store the final result in the cache
        self._cache[key] = tree, lastmark
        return tree

    return memoize_left_rec_wrapper
```

The algorithm works step by step as follows.

#### Step A: Planting the seed (preventing infinite recursion)

When a left-recursive rule (for example `expr`) is first called at a position `mark`, a provisional "failure" result is written to the cache:

```python
self._cache[key] = None, mark
```

Here `key` is `(mark, 'expr', ())`. If `expr` is called again at the same position `mark` while `expr` is being parsed, this "failure" is a cache hit and `None` is returned immediately. This is the most important step: it breaks the chain of infinite recursion.

#### Step B: Growing by iteration

Next, the `while True:` loop grows the result iteratively.
On each iteration, the parser position is reset to the original start position `mark` and the rule body `method(self)` is run again.

- **First iteration**: the recursive call uses the "failure" cached in step A, so only the non-recursive alternatives of `expr` (for example `term`) match.
- **Later iterations**: if the cache was updated in step C, the grown result is used to try an even longer match.

#### Step C: Checking progress and updating the cache

After each attempt, progress is checked with `if not result or endmark <= lastmark:`.

- If the parse failed, or did not advance the input beyond the previous end (`lastmark`), no longer match is possible and the loop exits.
- **On progress (`endmark > lastmark`)**: a longer match was found (for example, from `term` to `term + term`). The new `result` and `endmark` are saved in `lastresult` and `lastmark`, and **the cache entry `self._cache[key]` is also overwritten with this new successful result**. This is the process of "growing the seed".

#### Step D: Finalizing the result

When the loop ends, `lastresult` holds the longest left-recursive match starting at that position. The parser position is reset to its end position `lastmark`, and `lastresult` is returned as the final result.

## 5. Summary

pegen supports left recursion by combining static analysis with dynamic resolution:

- **At parser generation time**: left-recursive rules are identified in advance using a dependency graph and SCC detection, and are marked with the `left_recursive` and `leader` flags.
- **At parse time**: when a leader rule is called, the `@memoize_left_rec` decorator applies the growing-the-seed algorithm. This solves two problems at once: (1) infinite recursion is prevented by caching a failure, and (2) the longest match is found by iteration.
