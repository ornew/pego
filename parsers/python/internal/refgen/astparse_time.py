# Times CPython's ast.parse on files (the reference of the benchmarks of the module):
#
#   python3.14 -I internal/refgen/astparse_time.py testdata/bench/_pydecimal.py testdata/bench/typing.py
#
# For each file it prints the best and the median of 30 runs after 30 warm-up runs.
import ast
import sys
import time

for path in sys.argv[1:]:
    with open(path, encoding="utf-8") as f:
        src = f.read()
    for _ in range(30):
        ast.parse(src)
    times = []
    for _ in range(30):
        t = time.perf_counter()
        ast.parse(src)
        times.append(time.perf_counter() - t)
    times.sort()
    print(f"{path}: best {times[0] * 1e3:.2f} ms, median {times[len(times) // 2] * 1e3:.2f} ms ({len(src) / times[len(times) // 2] / 1e6:.1f} MB/s)")
