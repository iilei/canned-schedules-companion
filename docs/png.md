# The week as a PNG

`preview render` writes the week as `schedule.svg`. To use it outside the forge, for example on a slide or in a chat
message, convert it with `rsvg-convert`. Both recipes expect `schedule.svg` in the current folder. They install
`font-ubuntu` for Ubuntu Mono, which every SVG names (newer canned-schedules releases name Noto Sans Mono before it),
and `ttf-dejavu` for the `→` that Ubuntu Mono lacks. So the PNG looks the same whichever release wrote the SVG. To
draw it in Noto Sans Mono, as the playground shows it, install `font-noto` (about 9 MiB on Alpine; its Noto Sans Mono has the
`→`) instead of both.

The SVG decides the theme. `preview render` writes one that follows the viewer, and `rsvg-convert` draws that in light
colours. For a dark PNG, write the SVG with the
[canned-schedules](https://github.com/iilei/canned-schedules) CLI first:

```sh
canned-schedules --svg-theme dark --svg-output-file schedule.svg schedule.md > /dev/null
```

<details>
<summary>Nonroot Wolfi image: build it once, then render without network access</summary>

```dockerfile
# Tested with cgr.dev/chainguard/wolfi-base@sha256:580c9ae79c0229cc381475fbfbed0d91fab5d6606e57cd78732f9836de109e90
FROM cgr.dev/chainguard/wolfi-base
RUN apk add --no-cache rsvg-convert fontconfig font-ubuntu ttf-dejavu
USER 65532:65532
ENTRYPOINT ["rsvg-convert"]
```

```sh
docker build -t schedule-png .

docker run --rm --network none --read-only --tmpfs /tmp \
  --cap-drop ALL --security-opt no-new-privileges \
  --user "$(id -u):$(id -g)" -e HOME=/tmp \
  -v "$PWD:/work" \
  schedule-png --zoom 2 --background-color white \
  --output /work/schedule.png /work/schedule.svg
```

The PNG belongs to your own user.

</details>

<details>
<summary>Alpine one-off: nothing to build, but it runs as root</summary>

```sh
docker run --rm -v "$PWD:/work" alpine:3.22 sh -c '
  apk add --no-cache --quiet rsvg-convert fontconfig font-ubuntu ttf-dejavu
  rsvg-convert --zoom 2 --background-color white \
    --output /work/schedule.png /work/schedule.svg
'
```

The PNG belongs to `root`.

</details>
