# Termium's encoder dependency

Source: `github.com/codr1/go-sixel` at `bf66418a4746` (module version
`v0.0.0-20260314180759-bf66418a4746`), derived from mattn/go-sixel.
The original MIT license is retained in `LICENSE`.

This checked-in module carries the fixes needed by the rendering pipeline:

- Widen the internal palette slot before adding one, preserving Plan9 white.
- Use terminal color registers 0–255; the internal transparency slot does not
  consume a terminal register.
- Format raster dimensions and repeat counts beyond three decimal digits.
- Preserve writer failures and short writes.
- Own fixed-palette caches per encoder, reset on palette changes, and cap entries
  at 65,536. Opaque websafe pixels use a direct RGB calculation instead of a map,
  and RGBA pixel loops avoid allocating color interfaces.

The client uses one preparation worker and encodes into bounded memory before
writing the terminal. Encoder instances are not safe for concurrent use.
Regression tests live in `client/frame_pipeline_test.go` and run under `npm test`.
When updating the dependency, retain these behaviors and their pixel/output tests.
