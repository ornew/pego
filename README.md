# PEGO: Pure Golang Parser Generator

## Example: Calculator

Generate parser from [grammer.json](./examples/calc/grammer.json):

```bash
go run ./examples/calc/cmd/gen
```

Parse by generated [parser.go](./examples/calc/parser.go):

```bash
go run ./examples/calc/cmd/parse
```

Given:

```
1+2*(3-4)/5
```

Get:

```
+ expr #1 (0:0-0:11): "1+2*(3-4)/5"
  + additive #2 (0:0-0:11): "1+2*(3-4)/5"
    + multiplicative #3 (0:0-0:1): "1"
      + value #6 (0:0-0:1): "1"
        + number #8 (0:0-0:1): "1"
    + binary_op1 #4 (0:1-0:2): "+"
    + multiplicative #3 (0:2-0:11): "2*(3-4)/5"
      + value #6 (0:2-0:3): "2"
        + number #8 (0:2-0:3): "2"
      + binary_op2 #5 (0:3-0:4): "*"
      + value #6 (0:4-0:9): "(3-4)"
        + group #7 (0:4-0:9): "(3-4)"
          + additive #2 (0:5-0:8): "3-4"
            + multiplicative #3 (0:5-0:6): "3"
              + value #6 (0:5-0:6): "3"
                + number #8 (0:5-0:6): "3"
            + binary_op1 #4 (0:6-0:7): "-"
            + multiplicative #3 (0:7-0:8): "4"
              + value #6 (0:7-0:8): "4"
                + number #8 (0:7-0:8): "4"
      + binary_op2 #5 (0:9-0:10): "/"
      + value #6 (0:10-0:11): "5"
        + number #8 (0:10-0:11): "5"
```
