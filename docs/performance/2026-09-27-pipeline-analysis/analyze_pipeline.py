import json,pathlib,collections,statistics,sys
from analyze_chrome import summary

def union(items):
    out=[]
    for a,b in sorted(items):
        if b<=a:continue
        if out and a<=out[-1][1]:out[-1]=(out[-1][0],max(b,out[-1][1]))
        else:out.append((a,b))
    return out

def length(items):return sum(b-a for a,b in union(items))
def intersection(a,b):
    return [(max(x,u),min(y,v)) for x,y in a for u,v in b if max(x,u)<min(y,v)]
def analyze(root):
    root=pathlib.Path(root);t=json.loads((root/'client.json.timeline.json').read_text());c=json.loads((root/'client.json').read_text())
    lo,hi=t['start_us'],t['end_us'];groups=collections.defaultdict(list);full=collections.defaultdict(list);frames=collections.defaultdict(dict)
    for e in t['events']:
        a,b=e['start_us'],e['end_us']
        if a<hi and b>lo:groups[e['name']].append((max(a,lo),min(b,hi)))
        if lo<=a<=b<hi:
            full[e['name']].append((b-a)/1000)
            if e.get('frame_us'):frames[e['frame_us']][e['name']]=e
    pct=lambda a:100*length(a)/(hi-lo)
    durations={n:summary(v) for n,v in full.items()}
    result={'client':c,'dropped':t['dropped'],'window_us':[lo,hi],'occupancy_pct':{n:pct(v) for n,v in groups.items()},'durations_ms':durations,
        'rpc_prepare_overlap_pct':pct(intersection(groups['capture.rpc'],groups['prepare.work'])),
        'prep_wait_during_rpc_pct':pct(intersection(groups['capture.rpc'],groups['prepare.wait'])),
        'prep_wait_during_pacing_pct':pct(intersection(groups['capture.pacing'],groups['prepare.wait'])),
        'no_rpc_prepare_write_pct':100-pct(groups['capture.rpc']+groups['prepare.work']+groups['display.write'])}
    queue=[];ui=[];ages=[];complete=[]
    for f,events in frames.items():
        if all(n in events for n in ['capture.rpc','prepare.work','display.write']):
            rpc,prep,write=[events[n] for n in ['capture.rpc','prepare.work','display.write']]
            queue.append((prep['start_us']-rpc['end_us'])/1000);ui.append((write['start_us']-prep['end_us'])/1000);ages.append((write['end_us']-f)/1000)
            complete.append({'frame_us':f,'rpc':rpc,'prep':prep,'write':write})
    result.update(queue_ms=summary(queue),prepare_to_write_ms=summary(ui),frame_age_ms=summary(ages),complete_frames=len(complete))
    if (root/'client.json.server.timeline.json').exists():
        s=json.loads((root/'client.json.server.timeline.json').read_text());residual=[]
        for e in t['events']:
            if e['name']!='capture.rpc' or not(lo<=e['start_us']<e['end_us']<hi):continue
            match=[p for p in s['spans'] if p['name']=='server.handler' and e['start_us']<=p['start_us'] and p['start_us']+p['duration_us']<=e['end_us']]
            if len(match)==1:residual.append((e['end_us']-e['start_us']-match[0]['duration_us'])/1000)
        result['rpc_minus_handler_ms']=summary(residual)
    result['frames']=complete
    return result
if __name__=='__main__':
    r=analyze(sys.argv[1]);pathlib.Path(sys.argv[2]).write_text(json.dumps(r,indent=2));print(json.dumps({k:v for k,v in r.items() if k not in ['frames','client']},indent=2))
