package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"github.com/ornew/pego/internal/syntax"
)

var sparseMemoSynthetic = func() string {
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

// BenchmarkSparseMemoTypeScript measures warm pooled parsing. The storage
// metrics include bit buffers, full arena chunks, and directory capacity;
// they exclude memo entries and the containing memo table.
func BenchmarkSparseMemoTypeScript(b *testing.B) {
	data, err := os.ReadFile("../../parsers/typescript/typescript.pego")
	if err != nil {
		b.Fatal(err)
	}
	g, err := syntax.Parse(string(data))
	if err != nil {
		b.Fatal(err)
	}
	prog, err := Compile(g, Options{})
	if err != nil {
		b.Fatal(err)
	}
	sources := []struct{ name, text string }{{"synthetic", sparseMemoSynthetic}, {"small", "const 日本語 = 1 + 2;\n"}}
	if pkg := os.Getenv("PEGO_TYPESCRIPT"); pkg != "" {
		data, err := os.ReadFile(filepath.Join(pkg, "lib", "_tsc.js"))
		if err != nil {
			b.Fatal(err)
		}
		sources = append(sources, struct{ name, text string }{"compiler", string(data)})
	}
	for _, source := range sources {
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, unit := range []Unit{CodePoints, Bytes} {
				for _, recognize := range []bool{false, true} {
					name := "Parse"
					if recognize {
						name = "Recognize"
					}
					b.Run(source.name+"/"+name+"/"+backend.String()+"/"+unit.String(), func(b *testing.B) {
						opts := ParseOptions{Backend: backend, Unit: unit, Recognize: recognize}
						if _, err := prog.ParseWith("main", source.text, opts); err != nil {
							b.Fatal(err)
						}
						b.SetBytes(int64(len(source.text)))
						b.ReportAllocs()
						for b.Loop() {
							if _, err := prog.ParseWith("main", source.text, opts); err != nil {
								b.Fatal(err)
							}
						}
						b.StopTimer()
						target := prog
						if recognize {
							target, err = prog.recognizer()
							if err != nil {
								b.Fatal(err)
							}
						}
						p := newParser(target, source.text, unit, !recognize)
						p.deferMemo = true
						if _, err := target.run(p, backend, "main"); err != nil {
							b.Fatal(err)
						}
						storage := sparseMemoStorage(p.memo)
						b.ReportMetric(float64(storage), "seen-storage-B")
						if !p.memo.reset() {
							storage = 0
						}
						b.ReportMetric(float64(storage), "seen-retained-B")
					})
				}
			}
		}
	}
}

func sparseMemoStorage(m *memoTable) int {
	size := cap(m.seen)*8 + cap(m.pages)*int(unsafe.Sizeof(seenRulePages{}))
	size += len(m.bitPages.chunks)*64*16*8 + cap(m.bitPages.chunks)*int(unsafe.Sizeof(uintptr(0)))
	for _, dir := range m.pages {
		size += cap(dir.pages) * int(unsafe.Sizeof(uintptr(0)))
	}
	return size
}
