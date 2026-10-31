package tsgen_test

// Exported alongside trace controls by TestExportGeneratedTSTraceBenchmarks.
// Reuse one sink per operation and exclude snapshot serialization from timing.
const generatedTSProfileBenchmark = `import * as p from "./parser.ts";
import {performance} from "node:perf_hooks";
const input=INPUT;
const duration=Number(process.env.ZP_PROFILE_BENCH_MS??150);
const warm=Number(process.env.ZP_PROFILE_WARM_MS??75);
const modes=(process.env.ZP_PROFILE_MODES??(OBSERVED?"none,trace,profile":"none")).split(",");
const operations=TYPED?["Parse","ParseAST","Recognize"]:["Parse","Recognize"];
let sink,eventSink;
const trace=e=>{eventSink=e.pos^e.end^e.evals};
for(const mode of modes)for(const op of operations)for(const unit of [p.CodePoints,p.Bytes]){
 const options={unit};
 if(mode==="trace")options.trace=trace;
 const profile=mode==="profile"?new p.Profile():null;
 if(profile!==null)options.profile=profile;
 const run=op==="Parse"?()=>p.parseWithOptions(input,options):op==="ParseAST"?()=>p.parseASTWithOptions(input,options):()=>({error:p.recognizeWithOptions(input,options)});
 const once=()=>{sink=run();if(sink.error!==null)throw sink.error};
 for(let until=performance.now()+warm;performance.now()<until;){for(let n=0;n<16;n++)once()}
 globalThis.gc?.();
 let count=0;const start=performance.now();let end=start;
 while(end-start<duration){for(let n=0;n<16;n++)once();count+=16;end=performance.now()}
 const metrics=profile?.snapshot();
 if(metrics!==undefined&&(metrics.calls===0||metrics.parses===0))throw new Error("profile was not active");
 process.stdout.write(JSON.stringify({op,mode,unit:unit===p.Bytes?"Bytes":"CodePoints",count,ns:(end-start)*1e6/count})+"\n");
}
if(sink===undefined||modes.includes("trace")&&eventSink===undefined)throw new Error("missing benchmark output");
`
