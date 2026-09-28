// Decode retained images after timing has ended. No decoder in measured sink.
import fs from 'node:fs/promises';
import path from 'node:path';
import puppeteer from 'puppeteer';
const directory=process.argv[2];
if(!directory)throw Error('Usage: node check-samples.mjs RUN_DIRECTORY');
const result=JSON.parse(await fs.readFile(path.join(directory,'result.json'),'utf8'));
const browser=await puppeteer.launch({headless:true,pipe:true,defaultViewport:null});
try{
 const page=await browser.newPage();
 const rows=[];
 for(const sample of result.samples){
  const image=await fs.readFile(path.join(directory,sample.file));
  const decoded=await page.evaluate(async ({data,format})=>{
   const img=new Image();img.src=`data:image/${format};base64,${data}`;await img.decode();
   const c=document.createElement('canvas');c.width=384;c.height=12;const ctx=c.getContext('2d',{willReadFrequently:true});ctx.drawImage(img,0,0);
   const pix=ctx.getImageData(0,0,384,12).data;let bits=0;let minContrast=255;
   for(let bit=0;bit<32;bit++){const i=(6*384+bit*12+6)*4;const luma=(pix[i]+pix[i+1]+pix[i+2])/3;bits=(bits*2+(luma>127?1:0))>>>0;minContrast=Math.min(minContrast,Math.abs(luma-127));}
   return {width:img.naturalWidth,height:img.naturalHeight,sync:bits>>>16,frame:bits&65535,minContrast};
  },{data:image.toString('base64'),format:result.options.format});
  if(decoded.sync!==0xA55A || decoded.minContrast<60)throw Error(`Invalid marker ${sample.file}: ${JSON.stringify(decoded)}`);
  if(decoded.width!==result.options.width||decoded.height!==result.options.height)throw Error(`Wrong image dimensions ${sample.file}: ${JSON.stringify(decoded)}`);
  const logicalEpoch=result.fixture.originEpochMS+decoded.frame/30*1000;
  const age=sample.receiveEpochMS-logicalEpoch;
  if(age < -5 || age>60000)throw Error(`Invalid marker age ${age} in ${sample.file}`);
  rows.push({...sample,...decoded,logicalAgeMS:age});
 }
 if(!rows.length)throw Error('No samples to validate');
 const ages=rows.map(x=>x.logicalAgeMS).sort((a,b)=>a-b);
 const summary={sampleCount:rows.length,logicalAgeMS:{mean:ages.reduce((a,b)=>a+b,0)/ages.length,p50:ages[Math.floor(ages.length/2)],p95:ages[Math.min(ages.length-1,Math.floor(ages.length*.95))]},distinctSampleFrames:new Set(rows.map(x=>x.frame)).size,rows,note:'Age of encoded logical 30Hz animation state at Node receipt, not precise compositor presentation or input latency. Logical time precedes actual drawing by 0-33ms under normal ticks and possibly longer under stalls. One sample per second; not a distribution over all delivered frames.'};
 await fs.writeFile(path.join(directory,'sample-validation.json'),JSON.stringify(summary,null,2)+'\n');
 console.log(JSON.stringify({directory,...summary,rows:undefined}));
}finally{await browser.close();}
