// Linux-only diagnostic accounting. Include threads via process stat; walk
// children of all threads because Chromium's zygotes need not fork on main TID.
import fs from 'node:fs/promises';
import {execFileSync} from 'node:child_process';
import {performance} from 'node:perf_hooks';
export async function createSampler(roots) {
  if (process.platform !== 'linux') throw Error('This proof-of-concept resource sampler requires Linux');
  const ticks=Number(execFileSync('getconf',['CLK_TCK'],{encoding:'utf8'}).trim());
  const pageBytes=Number(execFileSync('getconf',['PAGESIZE'],{encoding:'utf8'}).trim());
  if (!(ticks>0 && pageBytes>0)) throw Error('Invalid system accounting units');
  const seen=new Map();let timer,tail=Promise.resolve(),failure,startMS,peakRSS=0,samples=0;
  async function snapshot(initial=false) {
    const queue=[...roots],visited=new Set();let rss=0;
    while(queue.length){
      const pid=queue.shift();if(visited.has(pid))continue;visited.add(pid);
      try {
        const stat=await fs.readFile(`/proc/${pid}/stat`,'utf8');
        const f=stat.slice(stat.lastIndexOf(')')+2).trim().split(/\s+/);
        const key=`${pid}:${f[19]}`,cpu=Number(f[11])+Number(f[12]);
        let row=seen.get(key);
        if(!row){row={pid,initial:initial?cpu:0,last:cpu};seen.set(key,row);} else row.last=Math.max(row.last,cpu);
        rss+=Math.max(0,Number(f[21]))*pageBytes;
        const tids=await fs.readdir(`/proc/${pid}/task`);
        for(const tid of tids){
          try {const children=await fs.readFile(`/proc/${pid}/task/${tid}/children`,'utf8');queue.push(...children.trim().split(/\s+/).filter(Boolean).map(Number));}
          catch(error){if(!['ENOENT','ESRCH'].includes(error.code))throw error;}
        }
      } catch(error){if(!['ENOENT','ESRCH'].includes(error.code))throw error;}
    }
    peakRSS=Math.max(peakRSS,rss);samples++;
  }
  return {
    async start(){if(startMS!==undefined)throw Error('Sampler already started');await snapshot(true);startMS=performance.now();timer=setInterval(()=>{tail=tail.then(()=>snapshot()).catch(e=>{failure=e;});},500);},
    async stop(){clearInterval(timer);await tail;const endMS=performance.now();await snapshot();if(failure)throw failure;const cpuSeconds=[...seen.values()].reduce((n,r)=>n+r.last-r.initial,0)/ticks;return {cpuSeconds,accountingSeconds:(endMS-startMS)/1000,cpuPercentOneCore:100*cpuSeconds/((endMS-startMS)/1000),peakSumRSSBytes:peakRSS,samples,processes:seen.size,notes:'Node + descendant Chromium processes; process CPU ticks sampled every 500ms. Short-lived exited process tails may be missed. Summed RSS double-counts shared pages; not PSS. Includes sampler overhead and slight boundary skew.'};}
  };
}
