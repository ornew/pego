package cue

import (
	"os"
	"strings"
	"testing"
)

// benchInput is CUE of about 256 KB: the sources of the corpus (testdata/corpus.tar.gz) that have no package
// clause, imports or experiments, each as the value of a field. It is real CUE of every kind, but dense in
// the unusual (tests of the language), not like a configuration. CUE_BENCH_OUT=file writes it, for
// internal/refgen -bench to time the reference parser on the same input.
var benchInput = func() string {
	corpus := loadCorpus(&testing.B{})
	var b strings.Builder
	n := 0
	for _, c := range corpus {
		if b.Len() >= 256<<10 {
			break
		}
		if strings.Contains(c.src, "package ") || strings.Contains(c.src, "import ") || strings.Contains(c.src, "@experiment") ||
			strings.HasPrefix(c.result, "error: ") {
			continue
		}
		n++
		b.WriteString("s")
		b.WriteString(itoa(n))
		b.WriteString(": {\n")
		b.WriteString(c.src)
		b.WriteString("\n}\n")
	}
	return b.String()
}()

func itoa(n int) string {
	var buf [20]byte
	i := len(buf)
	for n >= 10 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	i--
	buf[i] = byte('0' + n)
	return string(buf[i:])
}

func TestMain(m *testing.M) {
	for env, src := range map[string]string{"CUE_BENCH_OUT": benchInput, "CUE_BENCH_CONFIG_OUT": configInput} {
		if out := os.Getenv(env); out != "" {
			if err := os.WriteFile(out, []byte(src), 0o644); err != nil {
				panic(err)
			}
		}
	}
	os.Exit(m.Run())
}

// configInput is a configuration of about 256 KB: definitions, constraints, disjunctions, comprehensions,
// interpolations and comments, repeated with different names, as a project of services would have it.
// CUE_BENCH_CONFIG_OUT=file writes it.
var configInput = func() string {
	var b strings.Builder
	b.WriteString(`package config

import (
	"list"
	"strings"
)

// A port of a service.
#Port: {
	name!: string
	port:  int & >=1 & <=65535
	proto: *"TCP" | "UDP"
	...
}

`)
	for i := 0; b.Len() < 256<<10; i++ {
		n := itoa(i)
		b.WriteString(`
// Service ` + n + ` and its deployment.
services: svc` + n + `: {
	name:    "svc` + n + `"
	version: "v1.` + n + `.0"
	replicas: *2 | int & >=1 & <=10
	ports: [#Port & {name: "http", port: 8080}, #Port & {name: "metrics", port: 9090, proto: "TCP"}]
	env: {
		LOG_LEVEL: *"info" | "debug" | "warn" // the default is info
		REGION:    "eu-west-` + itoa(i%3+1) + `"
		"X-TOKEN": "t\(name)-\(version)"
	}
	resources: requests: {cpu: "100m", memory: "128Mi"}
	if replicas > 3 {
		autoscale: {min: replicas, max: replicas * 2}
	}
	labels: {for k, v in env if k != "X-TOKEN" {"env.\(strings.ToLower(k))": v}}
	hosts: [for p in ports if p.proto == "TCP" {"\(name):\(p.port)"}]
	count: len(hosts) + list.Sum([1, 2, 3])
	ratio: 0.5 * 1.5e3 / 3Ki
	enabled: !(replicas < 1) && name =~ "^svc[0-9]+$"
	tags?: [...string]
	owner: string | *"team-` + itoa(i%7) + `" @go(Owner) @json(owner,omitempty)
}
`)
	}
	return b.String()
}()

func BenchmarkParseAST(b *testing.B) {
	for _, in := range []struct{ name, src string }{{"config", configInput}, {"corpus", benchInput}} {
		b.Run(in.name, func(b *testing.B) {
			b.SetBytes(int64(len(in.src)))
			b.ReportAllocs()
			for b.Loop() {
				if _, err := ParseAST(in.src); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkParseFile(b *testing.B) {
	for _, in := range []struct{ name, src string }{{"config", configInput}, {"corpus", benchInput}} {
		b.Run(in.name, func(b *testing.B) {
			b.SetBytes(int64(len(in.src)))
			b.ReportAllocs()
			for b.Loop() {
				if _, err := ParseFile(in.src); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkParse(b *testing.B) {
	for _, in := range []struct{ name, src string }{{"config", configInput}, {"corpus", benchInput}} {
		b.Run(in.name, func(b *testing.B) {
			b.SetBytes(int64(len(in.src)))
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Parse(in.src); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkRecognize(b *testing.B) {
	for _, in := range []struct{ name, src string }{{"config", configInput}, {"corpus", benchInput}} {
		b.Run(in.name, func(b *testing.B) {
			b.SetBytes(int64(len(in.src)))
			b.ReportAllocs()
			for b.Loop() {
				if err := Recognize(in.src); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
