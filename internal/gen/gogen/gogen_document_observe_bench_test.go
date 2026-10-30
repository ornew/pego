package gogen_test

// Exported by TestGeneratedDocumentBenchmarkFixture. Observation modes are
// generated only when the measured package includes their declarations.
const generatedDocumentObservationBenchmark = `package generateddocumentbench

import (
 "os"
 "strconv"
 "strings"
 "testing"
 "unicode/utf8"
 p "generateddocumentbench/PARSER"
)
var observationFUNCTIONResult VALUE
func BenchmarkObservationFUNCTION(b *testing.B){
 modes:=strings.Split(os.Getenv("PEGO_DOCUMENT_OBSERVE_MODES"),",")
 if len(modes)==1&&modes[0]=="" {modes=strings.Split(DEFAULTMODES,",")}
 for _,records:=range []int{256,4096}{for _,unit:=range []struct{name string;unit p.PositionUnit}{{"CodePoints",p.CodePoints},{"Bytes",p.Bytes}}{for _,mode:=range modes{for _,operation:=range []string{"Batch","Cold","Warm","Edit"}{
  b.Run(strconv.Itoa(records)+"/"+unit.name+"/"+mode+"/"+operation,func(b *testing.B){
   input:=INPUT
   opts:=[]p.ParseOption{p.WithUnit(unit.unit)}
   MODECHECK
   SETUP
   d,err:=p.NewDocument(input,p.WithUnit(unit.unit));if err!=nil{b.Fatal(err)}
   value,err:=d.METHOD();if err!=nil||!(VALID){b.Fatal("preflight",err)}
   pos:=len(input)-2;if unit.unit==p.CodePoints {pos=utf8.RuneCountInString(input)-2}
   b.ReportAllocs();b.ResetTimer()
   for i:=0;i<b.N;i++{
    var value VALUE
    var err error
    switch operation {
    case "Batch":value,err=p.METHOD(input,opts...)
    case "Cold":
     fresh,createErr:=p.NewDocument(input,p.WithUnit(unit.unit));if createErr!=nil{b.Fatal(createErr)}
     value,err=fresh.METHOD(opts...)
    case "Warm":value,err=d.METHOD(opts...)
    case "Edit":
     replacement:="ALTERNATE";if i&1!=0 {replacement="ORIGINAL"}
     if err:=d.Edit(pos,pos+1,replacement);err!=nil{b.Fatal(err)}
     value,err=d.METHOD(opts...)
    }
    if err!=nil||!(VALID){b.Fatal("observed operation",err)}
    observationFUNCTIONResult=value
   }
   b.StopTimer()
   CHECK
  })
 }}}}
}
`
