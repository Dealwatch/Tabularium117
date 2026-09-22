# Third-party notices

Tabularium 117 is MIT-licensed (see `LICENSE`). It incorporates data and reference
code from the third-party projects below.

## anno-mods/anno-117-calculator

- **What we take:** GUID → name/category/region data baked into
  `internal/catalog/catalog.json` (products, buildings, workforce types,
  regions, sessions, with names in all 12 languages the calculator ships).
  Regenerated with `tools/gen-catalog` from the project's `js/params.js`.
- **Revision:** `16ce9aa4e49c7baac45d8c3f75da6d2ba27ab564`, dated 2026-09-19.
- **License:** MIT.

```
MIT License

Copyright (c) 2019-2024 Nico Höllerich (NiHoel)

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

The calculator's own `LICENSE` file additionally notes that all game assets
(icons, images, building/product names, game balance values) are © Ubisoft
and not covered by its MIT license. Tabularium 117 takes only the GUID/name/
category/region mapping, never icon paths, in line with that notice and with
`KONZEPT.md` §7.

## github.com/Microsoft/go-winio v0.6.2

- **What we take:** the Go module, used unmodified by `internal/pipe` to
  open and read the game's named pipe on Windows.
- **License:** MIT.

```
The MIT License (MIT)

Copyright (c) 2015 Microsoft

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## github.com/skip2/go-qrcode v0.0.0-20200617195104-da1b6568686e

- **What we take:** the Go module, used unmodified by `internal/server` to
  render the QR code of the LAN address at `/api/v1/lan/qr.png` (KONZEPT.md
  section 6). It has no dependencies outside the standard library.
- **Author:** Tom Harwood.
- **License:** MIT.

```
Copyright (c) 2014 Tom Harwood

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
```

## modernc.org/sqlite v1.59.0

- **What we take:** the Go module, used unmodified by `internal/store` as the
  SQLite engine for the history database. It is a pure-Go translation of
  SQLite, so the release stays one static `tabularium117.exe` built with
  `CGO_ENABLED=0`.
- **License:** BSD-3-Clause.
- **Transitive modules:** `modernc.org/libc`, `modernc.org/mathutil` and
  `modernc.org/memory` are pulled in by it and carry the same BSD-3-Clause
  license with their own "The <Name> Authors" copyright lines. The remaining
  transitive modules are `github.com/dustin/go-humanize` (MIT),
  `github.com/google/uuid` (BSD-3-Clause), `github.com/mattn/go-isatty` (MIT),
  `github.com/ncruces/go-strftime` (MIT),
  `github.com/remyoudompheng/bigfft` (BSD-3-Clause) and `golang.org/x/sys`
  (BSD-3-Clause).

```
Copyright (c) 2017 The Sqlite Authors. All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice, this
list of conditions and the following disclaimer.

2. Redistributions in binary form must reproduce the above copyright notice,
this list of conditions and the following disclaimer in the documentation
and/or other materials provided with the distribution.

3. Neither the name of the copyright holder nor the names of its contributors
may be used to endorse or promote products derived from this software without
specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND
ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

## uPlot v1.6.32

- **What we take:** `dist/uPlot.esm.js` and `dist/uPlot.min.css`, vendored
  unmodified under `web/vendor/uplot/` and imported as an ES module by
  `web/js/views/history.js` for the history chart (KONZEPT.md section 3).
- **Author:** Leon Sorokin.
- **License:** MIT (`web/vendor/uplot/LICENSE`).

```
The MIT License (MIT)

Copyright (c) 2022 Leon Sorokin

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
```

## UbisoftMainzAnno/anno117_pipe_example and anno-mods/anno117-game-connector

- **What we take:** protocol knowledge documented in `docs/protocol.md`
  (pipe name, framing, message types) from the reference reader, and the
  captured fixture `testdata/connector/example_responses.txt` from the
  connector project, used for decoder and catalog tests. No code from either project is compiled into Tabularium 117.
- **License:** Unlicense (public domain dedication).

## Game content

Anno 117: Pax Romana, its game content, names, icons, and other assets are
© Ubisoft Entertainment. Tabularium 117 is an unofficial, community-made fan tool
and is not affiliated with, endorsed by, or sponsored by Ubisoft.
