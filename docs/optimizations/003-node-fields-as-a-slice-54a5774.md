# 3. Node Fields as a Slice (54A5774)

- `Node.Fields` was a `map[string]any`; each struct node paid for a map (several hundred bytes). It is now a slice of
  name/value pairs with `Get`; JSON output (sorted keys) and `Node.Field` are unchanged.
- Effect: JSON 72 → 64 MB.
