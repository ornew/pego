package tsgen_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

// Export standalone controls and raw measurements outside the repository.
func TestExportGeneratedTSTraceBenchmarks(t *testing.T) {
	t.Parallel()
	dir := os.Getenv("PEGO_TS_TRACE_BENCH_DIR")
	if dir == "" {
		t.Skip("set PEGO_TS_TRACE_BENCH_DIR to export TypeScript trace controls")
	}
	write := func(name string, data []byte) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	manifest := map[string]int{}
	for _, fixture := range []struct{ name, path, input string }{
		{"json", "parsers/json/json.pego", "[" + strings.TrimSuffix(strings.Repeat(`{"name":"éclair","n":123,"ok":true},`, 64), ",") + "]"},
		{"xml", "parsers/xml/xml.pego", "<root>" + strings.Repeat(`<item name="éclair">123</item>`, 64) + "</root>"},
		{"calc", "examples/calculator/calc.pego", strings.TrimSuffix(strings.Repeat("1+2*3+", 32), "+")},
	} {
		data, err := os.ReadFile(filepath.Join("../../..", fixture.path))
		if err != nil {
			t.Fatal(err)
		}
		g, err := parseTest(string(data))
		if err != nil {
			t.Fatal(err)
		}
		for _, typed := range []bool{false, true} {
			route := "node"
			if typed {
				route = "typed"
			}
			for _, enabled := range []bool{false, true} {
				variant := "ordinary"
				if enabled {
					variant = "observed"
				}
				files, err := gentest.GenerateTS(g, gen.GenOptions{Start: "main", Types: typed, Recognize: true, Observability: enabled})
				if err != nil {
					t.Fatal(err)
				}
				path := fixture.name + "/" + route + "/" + variant
				// The drivers run the files as generated.
				writeTSFiles(t, filepath.Join(dir, path), files)
				size := 0
				for _, f := range files {
					size += len(f.Content)
				}
				profileDriver := strings.NewReplacer("INPUT", fmt.Sprintf("%q", fixture.input), "OBSERVED", fmt.Sprint(enabled), "TYPED", fmt.Sprint(typed)).Replace(generatedTSProfileBenchmark)
				write(path+"/profile-bench.mjs", []byte(profileDriver))
				manifest[path] = size
				if typed {
					driver := strings.NewReplacer("INPUT", fmt.Sprintf("%q", fixture.input), "OBSERVED", fmt.Sprint(enabled)).Replace(generatedTSTraceBenchmark)
					write(path+"/bench.mjs", []byte(driver))
				}
			}
		}
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	write("manifest.json", data)
}

const generatedTSTraceBenchmark = `import * as p from "./parser.ts";
import {performance} from "node:perf_hooks";
const input=INPUT;
const duration=Number(process.env.PEGO_TRACE_BENCH_MS??200);
const warm=Number(process.env.PEGO_TRACE_WARM_MS??100);
let sink,eventSink,ruleSink;
const trace=e=>{eventSink=e.pos^e.end^e.evals;ruleSink=e.rule};
const operations=["Parse","ParseRule","ParseAST","Recognize","ParseLegacy","ParseASTLegacy","RecognizeLegacy"];
for(const mode of OBSERVED?["none","full","root","unknown"]:["none"]){
 for(const op of mode==="none"?operations:["Parse","ParseAST"]){
  for(const unit of [p.CodePoints,p.Bytes]){
   const options={unit};
   if(mode!=="none")options.trace=trace;
   if(mode==="root")options.traceMaxDepth=1;
   if(mode==="unknown")options.traceRules=["missing"];
   const run=op==="Parse"?()=>p.parseWithOptions(input,options):op==="ParseRule"?()=>p.parseRuleWithOptions("main",input,options):op==="ParseAST"?()=>p.parseASTWithOptions(input,options):op==="Recognize"?()=>({error:p.recognizeWithOptions(input,options)}):op==="ParseLegacy"?()=>p.parse(input,unit):op==="ParseASTLegacy"?()=>p.parseAST(input,unit):()=>({error:p.recognize(input,unit)});
   const once=()=>{sink=run();if(sink.error!==null)throw sink.error};
   for(let until=performance.now()+warm;performance.now()<until;){for(let n=0;n<16;n++)once()}
   globalThis.gc?.();
   let count=0;const start=performance.now();let end=start;
   while(end-start<duration){for(let n=0;n<16;n++)once();count+=16;end=performance.now()}
   process.stdout.write(JSON.stringify({op,mode,unit:unit===p.Bytes?"Bytes":"CodePoints",count,ns:(end-start)*1e6/count})+"\n");
  }
 }
}
if(sink===undefined||OBSERVED&&(eventSink===undefined||ruleSink===undefined))throw new Error("missing benchmark output");
`
