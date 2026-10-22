# Find Out Why a Grammar Is Slow

**Problem.** A grammar accepts the right input but parses it more slowly than it should, and you do not know which rule is
to blame. You want a measurement that points at a rule, a change that fixes it, and a second measurement that shows the
fix worked.

Run `pego profile` on a realistic input. It parses the input and reports, for each rule, how many times it was called,
how many of those calls did work, how much input the failed calls examined and threw away (`wasted`), and the time spent
in the rule itself. Counts do not depend on the machine, so use them to compare a grammar before and after a change; times
do.

The grammar reads lines of comma-separated items that end with `;` or `.`, and was written with one rule for each ending:

```pego
// slow.pego
@start
rule main = line* $$

rule line = spaced / plain

rule spaced = ws item (ws "," ws item)* ws ";" "\n"
rule plain = ws item (ws "," ws item)* ws "." "\n"

rule item = word (" "+ word)*
rule word = [a-z]+
rule ws = [ \t]*
```

Make an input of 20,000 lines, half ending in `;` and half in `.`, and profile it, showing the three most expensive rules:

```bash
awk 'BEGIN { for (i = 0; i < 20000; i++) print "alpha beta, gamma delta, epsilon zeta, eta theta, iota kappa" (i % 2 ? "." : ";") }' > big.txt
pego profile -g slow.pego -n 3 < big.txt
```

## Expected Output

```text
result: ok
examined 1240001 positions in 292.65ms (profiling included)
730019 calls, 680023 body evaluations, 49996 memo hits (7% of calls)
0 reused repetition elements (completed root calls)

    rule   calls   evals   memo  matched  failed  repeats  consumed  wasted      time     self  self%  reused
    item  150002  100006  49996   150000       2        5   1560000       1  137.47ms  86.94ms   29.7       0
  spaced   20001   20001      0    10000   10001        0    620000  610001  238.21ms  80.37ms   27.5       0
    word  200011  200011      0   200010       1       10    940047       1   50.53ms  50.53ms   17.3       0
(4 more rules; -n 0 shows all)

What to look at:
- Most time is spent in item (30%), spaced (27%), word (17%), not counting the rules they call.
- Evaluated again where they had been evaluated before: ws (130001 of 330002 evaluations, up to 4 times at 2:61). A memoized rule is usually evaluated at most twice at a position; a rule that is not is evaluated whenever its caller is retried there. Look for choices whose alternatives begin with the same calls, and factor the common part out.
- Failed after examining much input: spaced (61 positions from 2:1). A call that fails late throws its work away, and what is tried next often matches the same input again. Check whether an alternative shares a long prefix with a later one, and whether a cut (#cut) after the part that identifies an alternative can stop the parser from trying the others.
```

(The times differ from run to run; the counts do not.)

## How It Works

- `spaced` failed on 10,001 of its 20,001 calls, and those failures examined 610,001 positions and threw them away: the
  `wasted` column. Every line that ends with `.` is read as far as the end by `spaced`, which fails at the last character,
  and then read again by `plain`. The two rules begin with the same items, so half of the work is done twice.
- The profile says the same in words: a choice whose alternatives begin with the same calls should have the common part
  factored out. Here, only the last character differs, so make it a choice inside one rule:

```pego
// fast.pego
@start
rule main = line* $$

rule line = ws item (ws "," ws item)* ws ("." / ";") "\n"

rule item = word (" "+ word)*
rule word = [a-z]+
rule ws = [ \t]*
```

```bash
pego profile -g fast.pego -n 3 < big.txt
```

```text
result: ok
examined 1240001 positions in 206.10ms (profiling included)
540005 calls, 540005 body evaluations, 0 memo hits (0% of calls)
0 reused repetition elements (completed root calls)

  rule   calls   evals  memo  matched  failed  repeats  consumed  wasted      time     self  self%  reused
  line   20001   20001     0    20000       1        0   1240000       1  201.63ms  70.42ms   34.2       0
  item  100001  100001     0   100000       1        0   1040000       1  108.11ms  59.97ms   29.1       0
  word  200001  200001     0   200000       1        0    940000       1   48.13ms  48.13ms   23.4       0
(2 more rules; -n 0 shows all)

What to look at:
- Most time is spent in line (34%), item (29%), word (23%), not counting the rules they call.
- Nothing else stands out: no rule is evaluated more than twice at a position, and no failed call examines much input.
```

The calls drop from 730,019 to 540,005, the wasted input from 610,001 positions to 1, and the profile has nothing left to
point at. The grammar accepts the same input.

## Variations and Limits

- Profiling slows the parse down, so compare profiles with each other, and measure the real speed without it, for example
  with a Go benchmark of `Parse`.
- When the alternatives share a prefix but the shared part cannot be merged, a cut after the part that identifies an
  alternative stops the parser from trying the others; see the [rule expressions guide](../guide/rule-expressions.md).
- Use a sample of real input. A grammar can be fast on small inputs and slow on a long line, and the profile shows the
  rules that dominate for the input you give it.
- `pego explain` tells you why an input fails, and `pego trace` shows every call; see
  [Debugging and Profiling](../guide/debugging.md).
