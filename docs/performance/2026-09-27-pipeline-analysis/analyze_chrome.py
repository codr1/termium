import json, collections, statistics, pathlib

def summary(xs):
    xs=sorted(xs)
    if not xs:return None
    return {'n':len(xs),'mean':statistics.mean(xs),'p50':statistics.median(xs),'p95':xs[min(len(xs)-1,int(.95*len(xs)))]}

def analyze(folder):
    root=pathlib.Path(folder); server=json.loads((root/'client.json.server.timeline.json').read_text()); events=json.loads((root/'client.json.chromium.json').read_text())['traceEvents']
    clocks=[]
    for m in server['clock']:
        marker=next(e for e in events if e['name']=='clock_sync' and e.get('args',{}).get('sync_id')==m['id'])
        clocks.append({'id':m['id'],'offset_us':(m['before_us']+m['after_us'])/2-marker['ts'],'uncertainty_us':(m['after_us']-m['before_us'])/2})
    anchor=min(clocks,key=lambda c:c['uncertainty_us'])
    offset=anchor['offset_us']
    procs={e['pid']:e['args']['name'] for e in events if e['name']=='process_name'}
    browser=next(pid for pid,name in procs.items() if name=='Browser')
    select=[e for e in events if e['name'] in ['DevToolsSession::HandleCommand in Browser','WidgetBase::ForceRedraw','CopyOutputRequest','EncodeBitmapAsJpeg','EncodeBitmapAsPngFast','EncodeBitmapAsPngSlow','CopyOutputResultSenderImpl::SendResult']]
    selected=[]
    for e in select:
        e=dict(e);e['start_us']=e['ts']+offset;selected.append(e)
    selected.sort(key=lambda e:e['start_us'])
    pending={}; gpu_copies=[]
    for e in selected:
        if e['name']!='CopyOutputRequest' or procs.get(e['pid'])!='GPU Process':continue
        key=(e['pid'],json.dumps(e.get('id2',e.get('id')),sort_keys=True))
        if e['ph']=='b':pending[key]=e
        elif e['ph']=='e' and key in pending:
            b=pending.pop(key);gpu_copies.append((b['start_us'],e['start_us']))
    def first(name,lo,hi,predicate=lambda _:True):
        return next((e for e in selected if e['name']==name and lo<=e['start_us']<=hi and predicate(e)),None)
    frames=[]; missed=0
    for span in server['spans']:
        if span['name']!='cdp.screenshot':continue
        lo=span['start_us'];hi=lo+span['duration_us']
        if lo < server['actualStart'] or hi>server['actualEnd']:continue
        dispatch=first('DevToolsSession::HandleCommand in Browser',lo-1000,hi,lambda e:e.get('args',{}).get('method')=='Page.captureScreenshot')
        if not dispatch:missed+=1;continue
        redraw=first('WidgetBase::ForceRedraw',dispatch['start_us'],hi)
        copy=first('CopyOutputRequest',dispatch['start_us'],hi,lambda e:e['pid']==browser and e['ph']=='b')
        encode=next((e for e in selected if e['name'].startswith('EncodeBitmapAs') and dispatch['start_us']<=e['start_us']<=hi),None)
        if not (redraw and copy and encode):missed+=1;continue
        row={'id':span['id'],'start_us':lo,'end_us':hi,'cdp_ms':span['duration_us']/1000,
             'node_to_dispatch_ms':(dispatch['start_us']-lo)/1000,
             'dispatch_to_redraw_ms':(redraw['start_us']-dispatch['start_us'])/1000,
             'redraw_to_copy_request_ms':(copy['start_us']-redraw['start_us'])/1000,
             'copy_to_encode_ms':(encode['start_us']-copy['start_us'])/1000,
             'encode_ms':encode['dur']/1000,
             'encode_to_node_ms':(hi-encode['start_us']-encode['dur'])/1000,
             'encode_thread_cpu_ms':encode.get('tdur',0)/1000,
             'native':{k:{'start_us':v['start_us'],'duration_us':v.get('dur',0)} for k,v in [('dispatch',dispatch),('redraw',redraw),('copy_request',copy),('encode',encode)]}}
        if min(row[k] for k in row if k.endswith('_ms') and k!='node_to_dispatch_ms')<0:missed+=1;continue
        gpu=next(((a,b) for a,b in gpu_copies if copy['start_us']<=a and b<=encode['start_us']),None)
        if gpu:
            row.update(copy_request_to_gpu_ms=(gpu[0]-copy['start_us'])/1000,gpu_copy_lifetime_ms=(gpu[1]-gpu[0])/1000,gpu_done_to_encode_ms=(encode['start_us']-gpu[1])/1000)
        frames.append(row)
    stages={key:summary([r[key] for r in frames if key in r]) for key in frames[0] if key.endswith('_ms')} if frames else {}
    spans=collections.defaultdict(list)
    for e in server['spans']:
        if e['start_us']>=server['actualStart'] and e['start_us']+e['duration_us']<=server['actualEnd']:spans[e['name']].append(e['duration_us']/1000)
    cpu=collections.defaultdict(float)
    for e in events:
        if e.get('ph')=='X' and 'tdur' in e and server['actualStart']<=e['ts']+offset<=server['actualEnd']:
            cpu[(procs.get(e['pid'],'?'),e['name'])]+=e['tdur']/1000
    return {'clock':clocks,'clock_anchor':anchor,'clock_drift_us':clocks[-1]['offset_us']-clocks[0]['offset_us'],'matched_frames':len(frames),'unmatched_frames':missed,'stages_ms':stages,'server_ms':{n:summary(v) for n,v in spans.items()},'native_cpu_ms_overlapping':[(p,n,v) for (p,n),v in sorted(cpu.items(),key=lambda kv:-kv[1])[:40]],'frames':frames,'gpu':server['environment']['system']['gpu'],'span_window':[server['actualStart'],server['actualEnd']]}
if __name__=='__main__':
    import sys
    result=analyze(sys.argv[1]);pathlib.Path(sys.argv[2]).write_text(json.dumps(result,indent=2))
    print(json.dumps({k:v for k,v in result.items() if k not in ['frames','gpu','native_cpu_ms_overlapping']},indent=2))
