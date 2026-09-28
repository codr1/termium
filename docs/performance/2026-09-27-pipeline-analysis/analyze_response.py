import json,pathlib,statistics,collections
from analyze_chrome import summary
root=pathlib.Path('/tmp/termium-pipeline-profile/results')
for name in ['sixel-720','kitty-720','sixel-2160','kitty-2160']:
 d=root/name; a=json.loads((d/'chrome-analysis.json').read_text()); events=json.loads(next(d.glob('canvas-*/client.json.chromium.json')).read_text())['traceEvents'];offset=a['clock_anchor']['offset_us']
 procs={e['pid']:e['args']['name'] for e in events if e['name']=='process_name'}
 encoders={round(e['ts']):e for e in events if e['name'].startswith('EncodeBitmapAs') and e.get('ph')=='X'}
 tasks=[e for e in events if e['name']=='ThreadControllerImpl::RunTask' and e.get('ph')=='X' and procs.get(e['pid'])=='Browser']
 rows=[]
 for f in a['frames']:
  enc=f['native']['encode']; lo=enc['start_us']-offset;hi=lo+enc['duration_us'];encoder=encoders[round(lo)];candidate=[e for e in tasks if e['pid']==encoder['pid'] and e['tid']==encoder['tid'] and e['ts']<=lo and e['ts']+e['dur']>=hi]
  if not candidate:continue
  task=min(candidate,key=lambda e:e['dur'])
  rows.append({'pre_encode_ms':(lo-task['ts'])/1000,'post_encode_task_ms':(task['ts']+task['dur']-hi)/1000,'task_cpu_minus_encode_ms':task.get('tdur',0)/1000-f['encode_thread_cpu_ms'],'task_end_to_node_ms':(f['end_us']-offset-task['ts']-task['dur'])/1000,'task_args':task.get('args')})
 out={'n':len(rows),'summary':{k:summary([r[k] for r in rows]) for k in rows[0] if k.endswith('_ms') and k != 'child_cpu_ms'},'rows':rows}
 (d/'response-analysis.json').write_text(json.dumps(out,indent=2));print(name,out['summary']);print('args',rows[0]['task_args'])
