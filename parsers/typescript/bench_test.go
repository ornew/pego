package typescript_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/typescript"
)

// synthetic is a TypeScript source of about 256 KB made of repeated declarations, classes, generic functions,
// async code, destructuring and template literals, like the workloads of the benchmarks of PEGO (bench/ in
// github.com/ornew/pego). It needs nothing but Go.
var synthetic = func() string {
	var b strings.Builder
	b.WriteString("import { Foo, type Bar } from \"./lib\";\n")
	for i := 0; b.Len() < 256<<10; i++ {
		fmt.Fprintf(&b, `
export interface Shape%[1]d<T extends object = {}> extends Base<T> {
  readonly id: number;
  name?: string;
  area(scale: number): number;
  [key: string]: unknown;
}

export type Pair%[1]d<K extends string, V> = { [P in K]: V } | Array<[K, V]> | (K extends "a" ? V : never);

export enum Color%[1]d { Red = 1, Green = Red << 1, Blue = "b".length }

export class Counter%[1]d<T> extends Base<T> implements Shape%[1]d<T> {
  private count = 0;
  static readonly instances: Counter%[1]d<unknown>[] = [];
  constructor(public readonly id: number, protected name: string = "counter") {
    super();
    Counter%[1]d.instances.push(this as Counter%[1]d<unknown>);
  }
  get value(): number { return this.count; }
  area(scale: number): number { return scale * scale * Math.PI; }
  async *stream(items: Iterable<T>, { limit = 10, ...rest }: { limit?: number } = {}): AsyncGenerator<T> {
    let n = 0;
    for (const item of items) {
      if (n++ >= limit) break;
      yield item;
    }
  }
}

export async function fetchAll%[1]d<T>(urls: string[], map: (x: unknown) => T): Promise<T[]> {
  const results: T[] = [];
  for (const url of urls) {
    try {
      const res = await fetch(url, { method: "GET", headers: { "x-id": `+"`id-${%[1]d}-${url.length}`"+` } });
      const body = (await res.json()) as { items?: unknown[] };
      results.push(...(body.items ?? []).map(map));
    } catch (e) {
      console.error(e instanceof Error ? e.message : String(e));
    } finally {
      results.length > 100 && results.splice(0, 50);
    }
  }
  return results.filter((x): x is NonNullable<T> => x != null);
}

const table%[1]d = new Map<string, number>([["a", 1], ["b", 2]]);
const total%[1]d = [...table%[1]d.values()].reduce((a, b) => a + b * 2 ** 3, 0) / (1 + %[1]d) %% 7;
`, i)
	}
	return b.String()
}()

type benchSource struct{ name, text string }

// benchSources returns the synthetic source and, when PEGO_TYPESCRIPT is set (the directory of the typescript
// package), large real ones: its declaration files, and the JavaScript of the compiler itself, which is valid
// TypeScript and the largest body of real code at hand.
func benchSources(b *testing.B) []benchSource {
	out := []benchSource{{"synthetic.ts", synthetic}}
	add := func(dir, rel string) {
		if dir == "" {
			return
		}
		data, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			b.Logf("skipping %s: %v", rel, err)
			return
		}
		out = append(out, benchSource{filepath.Base(rel), string(data)})
	}
	pkg := os.Getenv("PEGO_TYPESCRIPT")
	add(pkg, "lib/lib.es5.d.ts")
	add(pkg, "lib/lib.dom.d.ts")
	add(pkg, "lib/typescript.d.ts")
	add(pkg, "lib/_tsc.js") // the compiler's own bundle: 6 MB of code, which is valid TypeScript
	return out
}

func benchEach(b *testing.B, f func(b *testing.B, s benchSource)) {
	for _, s := range benchSources(b) {
		b.Run(s.name, func(b *testing.B) {
			b.SetBytes(int64(len(s.text)))
			b.ReportAllocs()
			f(b, s)
		})
	}
}

// BenchmarkParseAST builds the typed AST.
func BenchmarkParseAST(b *testing.B) {
	benchEach(b, func(b *testing.B, s benchSource) {
		for b.Loop() {
			if _, err := typescript.ParseAST(s.text); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkRecognize only checks the source.
func BenchmarkRecognize(b *testing.B) {
	benchEach(b, func(b *testing.B, s benchSource) {
		for b.Loop() {
			if err := typescript.Recognize(s.text); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkParse builds the generic tree of Nodes.
func BenchmarkParse(b *testing.B) {
	benchEach(b, func(b *testing.B, s benchSource) {
		for b.Loop() {
			if _, err := typescript.Parse(s.text); err != nil {
				b.Fatal(err)
			}
		}
	})
}
