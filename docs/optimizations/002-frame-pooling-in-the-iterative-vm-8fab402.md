# 2. Frame pooling in the iterative VM (8fab402)

- Call frames, body frames and body state were heap objects per rule call. They now come from a per-parser free list.
- Effect: iterative VM allocation went from ~2.3× the recursive VM to parity.
