# yaml-test-suite

The test directories and `License` are copied from [yaml/yaml-test-suite](https://github.com/yaml/yaml-test-suite),
data release `data-2022-01-17` (commit 6ad3d2c62885d82fc349026c136ef560838fdf3d of the `data` branch, MIT License).
Each directory is a test ID, some with numbered subtests, and holds the files of the release that the tests use:

| File | Contents |
|:--|:--|
| `===` | The name of the test |
| `in.yaml` | The input |
| `test.event` | The expected event stream |
| `in.json` | The expected values as JSON, one per document, where the test has them |
| `error` | Present if the input must be rejected |

The `name/` and `tags/` directories of the release, which only link to the test directories, and the files the tests do
not use (`out.yaml`, `emit.yaml`, `lex.token`) are left out.
