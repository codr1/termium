# Analysis inputs and replay

`summary.json` preserves aggregate evidence without the large browser traces. The HTML timeline beside this directory embeds the measured Go and browser phase spans. Raw files are retained at `/tmp/termium-pipeline-profile/results` and on ea at `/tmp/termium-pipeline-ea-fGpsxF/results`.

The scripts use Python standard library only. `analyze_pipeline.py RUN_DIR OUTPUT_JSON` processes a run containing `client.json` and `client.json.timeline.json`. `analyze_chrome.py RUN_DIR OUTPUT_JSON` additionally needs `.chromium.json` and `.server.timeline.json`. Run-directory examples are `results/sixel-2160/canvas-sixel-1`.

`analyze_response.py` and `analyze_node.py` currently use the investigation's absolute result-root path near the top; change that single path to replay another copy of these artifacts. The response script consumes each run's previously generated `chrome-analysis.json` and checks that enclosing tasks belong to the encoder's same process/thread.

The timeline recorder closes unfinished spans at the measurement boundary. Duration statistics exclude all events ending exactly at that boundary; occupancy includes their clipped portions. Browser stage attribution assumes this pipeline's single outstanding screenshot request and one measured page. It is not a general multi-tab trace analyzer. Independent cumulative CPU scopes overlap and must not be summed.
