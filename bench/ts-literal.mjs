// Run the embedded TypeScript literal matcher without rule/input setup or trees.
// Requires a Node.js version that runs TypeScript directly. An optional path
// selects another checkout's runtime.ts for before/after comparisons.
import { readFileSync, writeFileSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";

const runtimePath = process.argv[2] ?? new URL("../internal/engine/tsrt/runtime.ts", import.meta.url);
const source = readFileSync(runtimePath, "utf8");
const helper = String.raw`
export function benchmarkLiteralMatchers() {
 for(const text of ["keyword","日本語","�","é�x"]){
  const chars=Array.from(text);
  const lit:Lit={text,cps:chars.map(x=>x.codePointAt(0)!),bytes:Array.from(encodeUTF8(text))};
  for(const match of [true,false]){
   const input=encodeUTF8(match?text:chars.slice(0,-1).join("")+"!");
   for(const unit of [Bytes,CodePoints]){
    const p=new Parser(input,unit,0);p.silent=1;
    const once=()=>{p.pos=0;const got=p.matchLiteral(lit,0,false)!==undefined;if(got!==match)throw new Error("incorrect match")};
    for(let i=0;i<100000;i++)once();
    let count=0;const begin=performance.now();let elapsed=0;
    do{for(let i=0;i<10000;i++)once();count+=10000;elapsed=performance.now()-begin;}while(elapsed<200);
    console.log(JSON.stringify({text,match,unit,ns:elapsed*1e6/count}));
   }
  }
 }
}
benchmarkLiteralMatchers();
`;
const dir = mkdtempSync(join(tmpdir(), "pego-ts-literal-"));
try {
  const path = join(dir, "matcher.ts");
  writeFileSync(path, source + helper);
  const result = spawnSync(process.execPath, [path], { stdio: "inherit" });
  if (result.error) throw result.error;
  process.exitCode = result.status ?? 1;
} finally {
  rmSync(dir, { recursive: true, force: true });
}
