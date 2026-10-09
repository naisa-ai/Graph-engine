#!/bin/sh
# Verify files without executing an image, including images for another architecture.
set -eu
image=$1
expected_revision=${2:-}
directory=$(mktemp -d)
container=
cleanup() {
    if [ -n "$container" ]; then docker rm "$container" >/dev/null; fi
    rm -rf "$directory"
}
trap cleanup EXIT HUP INT TERM
container=$(docker create --entrypoint /bin/true "$image")
docker cp "$container:/usr/share/doc/graph-engine" "$directory/doc"
docker cp "$container:/app/graph-engined" "$directory/graph-engined"
docker cp "$container:/usr/local/lib" "$directory/lib"
python3 "$(dirname "$0")/package_compliance.py" verify \
    --output "$directory/doc" --binary "$directory/graph-engined" \
    --library-root "$directory/lib" --expected-revision "$expected_revision"
