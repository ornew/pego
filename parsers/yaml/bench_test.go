package yaml_test

import (
	stdjson "encoding/json"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/yaml"
)

// benchInput is a stream of Kubernetes-like manifests of about 256 KB: deployments, services and
// config maps, with nested block mappings and sequences, flow sequences, quoted strings, comments and
// literal block scalars.
var benchInput = func() string {
	words := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta"}
	r := rand.New(rand.NewPCG(1, 2))
	w := func() string { return words[r.IntN(len(words))] }
	var b strings.Builder
	for i := 0; b.Len() < 256<<10; i++ {
		name := fmt.Sprintf("%s-%s-%d", w(), w(), i)
		fmt.Fprintf(&b, `---
# Deployment %d
apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  namespace: %s
  labels:
    app.kubernetes.io/name: %s
    app.kubernetes.io/part-of: "%s"
  annotations:
    deployment.kubernetes.io/revision: "%d"
spec:
  replicas: %d
  selector:
    matchLabels:
      app: %s
  template:
    metadata:
      labels:
        app: %s
    spec:
      containers:
        - name: %s
          image: registry.example.com/%s/%s:v1.%d.%d
          args: ["--port=8080", "--log-level=%s", '--name=%s']
          ports:
            - containerPort: 8080
              protocol: TCP
          env:
            - name: MODE
              value: %s
            - name: RATIO
              value: "0.%d"
          resources:
            limits: {cpu: 500m, memory: 128Mi}
            requests: {cpu: 250m, memory: 64Mi}
          readinessProbe:
            httpGet:
              path: /healthz
              port: 8080
            initialDelaySeconds: %d
---
apiVersion: v1
kind: Service
metadata:
  name: %s
spec:
  type: ClusterIP
  selector:
    app: %s
  ports:
  - port: 80
    targetPort: 8080  # the container port
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: %s-config
data:
  enabled: "true"
  config.yaml: |
    server:
      port: 8080
      name: %s
    features: [%s, %s]
`, i, name, w(), name, w(), r.IntN(100), 1+r.IntN(5), name, name, name, w(), w(), r.IntN(10), r.IntN(10),
			w(), name, w(), r.IntN(100), r.IntN(30), name, name, name, name, w(), w())
	}
	return b.String()
}()

func BenchmarkParseAST(b *testing.B) {
	b.SetBytes(int64(len(benchInput)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := yaml.ParseAST(benchInput); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLoadAll(b *testing.B) {
	b.SetBytes(int64(len(benchInput)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := yaml.LoadAll(benchInput); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEvents(b *testing.B) {
	b.SetBytes(int64(len(benchInput)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := yaml.Events(benchInput); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRecognize(b *testing.B) {
	b.SetBytes(int64(len(benchInput)))
	b.ReportAllocs()
	for b.Loop() {
		if err := yaml.Recognize(benchInput); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParse(b *testing.B) {
	b.SetBytes(int64(len(benchInput)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := yaml.Parse(benchInput); err != nil {
			b.Fatal(err)
		}
	}
}

// The same documents as JSON, decoded by encoding/json, for comparison: the standard library has no
// YAML parser, and JSON is the closest format it reads.
func BenchmarkEncodingJSONUnmarshal(b *testing.B) {
	docs, err := yaml.LoadAll(benchInput)
	if err != nil {
		b.Fatal(err)
	}
	data, err := stdjson.Marshal(docs)
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		var v any
		if err := stdjson.Unmarshal(data, &v); err != nil {
			b.Fatal(err)
		}
	}
}
