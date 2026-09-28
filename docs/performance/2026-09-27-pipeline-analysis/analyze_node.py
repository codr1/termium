import json,pathlib,collections
root=pathlib.Path('/tmp/termium-pipeline-profile/results')
for file in sorted(root.glob('*/canvas-*/client.json.node.cpuprofile')):
 p=json.loads(file.read_text());nodes={n['id']:n for n in p['nodes']};weights=collections.defaultdict(int)
 assert len(p['samples'])==len(p['timeDeltas'])
 for nid,delta in zip(p['samples'],p['timeDeltas']):
  c=nodes[nid]['callFrame'];weights[(c['functionName'],c.get('url',''),c.get('lineNumber',-1))]+=delta
 total=sum(p['timeDeltas']);out={'window_us':p['endTime']-p['startTime'],'weighted_us':total,'samples':len(p['samples']),'self':[{'function':k[0],'url':k[1],'line':k[2]+1,'ms':v/1000,'percent_window':100*v/(p['endTime']-p['startTime'])}for k,v in sorted(weights.items(),key=lambda kv:-kv[1])]}
 (file.parent.parent/'node-analysis.json').write_text(json.dumps(out,indent=2));print(file.parent.parent.name,[(e['function'],e['ms'])for e in out['self'][:5]])
