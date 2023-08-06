# PEGO: Pure Golang Parser Generator

## Example: Calculator

```
package calc

expr <- term (term_binary_op term)*
term <- factor (factor_binary_op factor)*
term_binary_op <- "+" / "-"
factor_binary_op <- "*" / "/"
factor <- group / number
group <- "(" expr ")"
number <- [0-9]+
```

Generate parser from [grammer.pego](./examples/calc/grammer.pego):

```bash
go run ./cmd/pego ./examples/calc/grammer.pego
```

Testing parse by generated [parser_gen.go](./examples/calc/parser_gen.go):

```bash
go test ./examples/calc/
```

Given:

```
1+2*(3-4)/5
```

Get:

```
1+2*(3-4)/5
+ <root> #1 (0:0-0:11): "1+2*(3-4)/5"
  + expr #2 (0:0-0:11): "1+2*(3-4)/5"
    + term #3 (0:0-0:1): "1"
      + factor #6 (0:0-0:1): "1"
        + number #8 (0:0-0:1): "1"
    + term_binary_op #4 (0:1-0:2): "+"
    + term #3 (0:2-0:11): "2*(3-4)/5"
      + factor #6 (0:2-0:3): "2"
        + number #8 (0:2-0:3): "2"
      + factor_binary_op #5 (0:3-0:4): "*"
      + factor #6 (0:4-0:9): "(3-4)"
        + group #7 (0:4-0:9): "(3-4)"
          + expr #2 (0:5-0:8): "3-4"
            + term #3 (0:5-0:6): "3"
              + factor #6 (0:5-0:6): "3"
                + number #8 (0:5-0:6): "3"
            + term_binary_op #4 (0:6-0:7): "-"
            + term #3 (0:7-0:8): "4"
              + factor #6 (0:7-0:8): "4"
                + number #8 (0:7-0:8): "4"
      + factor_binary_op #5 (0:9-0:10): "/"
      + factor #6 (0:10-0:11): "5"
        + number #8 (0:10-0:11): "5"
```
