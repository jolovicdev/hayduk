# Third-party notices

The embedded UI in `internal/server/dist` ships pieces of third-party
software inside the hayduk binary. Their licenses, verbatim, live in
`docs/licenses/`; this file maps every bundled family to its notice.

| Bundled in the binary | Source | License | Notice |
| --- | --- | --- | --- |
| IBM Plex Sans woff2/woff/ttf (all weights, latin + cyrillic subsets) | `@fontsource/ibm-plex-sans` 5.3.x, fonts © IBM Corp. | SIL Open Font License 1.1 | [IBM-Plex-Sans-OFL-1.1.txt](licenses/IBM-Plex-Sans-OFL-1.1.txt) |
| IBM Plex Mono woff2/woff/ttf (all weights) | `@fontsource/ibm-plex-mono` 5.3.x, fonts © IBM Corp. | SIL Open Font License 1.1 | [IBM-Plex-Mono-OFL-1.1.txt](licenses/IBM-Plex-Mono-OFL-1.1.txt) |
| Phosphor icon webfonts (regular + fill: ttf/woff/svg) | `@phosphor-icons/web` 2.1.x, © Phosphor Icons | MIT | [Phosphor-Icons-MIT.txt](licenses/Phosphor-Icons-MIT.txt) |

The compiled application JavaScript additionally embeds code from:

- `solid-js` 1.9.x — MIT, https://github.com/solidjs/solid
- `@fontsource/*` packaging (css subset wiring) — MIT, https://fontsource.org

The MIT license texts above are reproduced as required for redistribution;
the OFL additionally requires the license to accompany the font files, which
these copies satisfy. hayduk itself is MIT (see LICENSE); this file does not
modify that.
