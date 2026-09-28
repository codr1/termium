import json,pathlib,statistics,sys
root=pathlib.Path(sys.argv[1]);rows=[]
for folder in sorted(root.glob('*-*-*')):
 if not folder.is_dir():continue
 r=json.loads((folder/'result.json').read_text());v=json.loads((folder/'sample-validation.json').read_text());receipt=json.loads((folder/'receipts.json').read_text());m=[e for e in receipt if e['phase']=='measurement']
 assert r['ok'] and not r['errors'],folder
 assert len(m)==r['counts']['measurement']
 assert all(0<=e['relMS']<=r['options']['seconds']*1000 for e in m)
 fps=len(m)/r['options']['seconds'];assert abs(fps-r['fps'])<.002
 assert v['distinctSampleFrames']==v['sampleCount'],folder
 rows.append({'run':folder.name,'mode':r['options']['mode'],'format':r['options']['format'],'height':r['options']['height'],'fps':fps,'bytesPerSec':r['bytesPerSec'],'bytesPerFrame':sum(e['bytes']for e in m)/len(m),'cpuPercent':r['sampler']['cpuPercentOneCore'],'cpuMSPerFrameApprox':r['sampler']['cpuPercentOneCore']*10/fps,'peakSumRSSMiB':r['sampler']['peakSumRSSBytes']/1048576,'sampleAgeMedianMS':v['logicalAgeMS']['p50'],'sampleAgeP95MS':v['logicalAgeMS']['p95'],'samples':v['sampleCount'],'screenshotRTTMS':r['screenshotRTTMS'],'payloadChanges':r['payloadChanges'],'frames':len(m)})
comparison=[]
for fmt in ['jpeg','png']:
 for height in [720,2160]:
  selected=[x for x in rows if x['format']==fmt and x['height']==height]
  if len(selected)!=4:continue
  a=[x for x in selected if x['mode']=='screenshot'];b=[x for x in selected if x['mode']=='stream']
  ratio=[b[i]['fps']/a[i]['fps'] for i in range(2)]
  comparison.append({'format':fmt,'height':height,'screenshot_fps':[x['fps']for x in a],'stream_fps':[x['fps']for x in b],'paired_ratios':ratio,'screenshot_cpu':[x['cpuPercent']for x in a],'stream_cpu':[x['cpuPercent']for x in b],'screenshot_sample_age_p50':[x['sampleAgeMedianMS']for x in a],'stream_sample_age_p50':[x['sampleAgeMedianMS']for x in b]})
out={'rows':rows,'comparisons':comparison};(root/'summary.json').write_text(json.dumps(out,indent=2)+'\n')
print(json.dumps(comparison,indent=2))
