# Graph Engine licensing

Copyright (c) 2025-2026 Naisa AI, Inc.

Naisa's server implementation and repository material without a separate
license notice are licensed under the GNU General Public License, version 2
or any later version (`GPL-2.0-or-later`). The complete GPLv2 text is in
[LICENSE](LICENSE), and the GPLv3 text is in [LICENSES/GPL-3.0.txt](LICENSES/GPL-3.0.txt).
This preserves the server license declared in this repository's README.

## Component licenses

| Component | License | License text |
| --- | --- | --- |
| Server implementation in `cmd/` and `internal/` | GPL-2.0-or-later | [GPLv2](LICENSE), [GPLv3](LICENSES/GPL-3.0.txt) |
| Go client, including generated client stubs | MIT | [clients/go/LICENSE](clients/go/LICENSE) |
| Python client, including generated client stubs | MIT | [clients/py/LICENSE](clients/py/LICENSE) |
| Protocol definitions in `proto/` | MIT | [proto/LICENSE](proto/LICENSE) |
| Generated protocol stubs in `gen/` | MIT | [gen/LICENSE](gen/LICENSE) |

Component licenses and existing file notices take precedence over the
repository default. Third-party software retains its own copyrights and
licenses. The root GPL license does not relicense the MIT components.

## Server distribution

The server links to [igraph](https://igraph.org/c/), which is licensed under
GPL-2.0-or-later. Both the Go service implementation and its C shim are part
of that linked server; the shim is not an exception to GPL requirements.

The server also links Apache-2.0 dependencies, including gRPC-Go and the
Prometheus Go client. Distribute this combination under GPLv3 using the
server's "or later" permission. Apache-2.0 is compatible with GPLv3, but not
GPLv2; see the [Apache Software Foundation's compatibility guidance](https://www.apache.org/licenses/GPL-compatibility).
Preserve the individual dependency notices as well.

For each server binary or container delivered to another party:

1. Provide the complete corresponding source for that specific version,
   including local modifications, required interface definitions, and build
   and installation scripts.
2. Include the applicable license texts and third-party notices, and provide
   corresponding source for covered dependencies, including the exact igraph
   source used by the build and any changes to it.
3. Put clear source retrieval instructions beside the binary or container
   download. Identify the exact source commit and dependency versions; a link
   to a moving `main` branch does not identify a particular release's source.
4. Preserve recipients' rights to modify and redistribute the GPL-covered
   software. Supply installation information when GPLv3 requires it for a
   User Product.

Publishing this repository supports source access, but does not by itself
establish compliance for every distributed binary, container, or historical
artifact. A release must be checked against its own source and notices.
See the [GNU GPL distribution FAQ](https://www.gnu.org/licenses/gpl-faq.en.html#DistributeExtendedBinary).

## Container source and notices

The production Dockerfile includes the following files under
`/usr/share/doc/graph-engine/` in each image:

- `COPYING`: the complete GPLv3 text for the combined server distribution.
- `NOTICE`: copyright, license scope, warranty and source extraction instructions.
- `THIRD_PARTY_NOTICES`: dependency copyright and license texts, including nested
  notices, and the independent MIT notices for the clients and interfaces.
- `BUILD_INFO.json`: application source hashes, optional source commit, compiler
  and build settings, exact Go/igraph/Debian versions and artifact checksums.
- `corresponding-source.tar.gz`: the application source and build scripts, exact
  Go module source archives, Go toolchain/standard-library source, igraph release
  source, and matching source packages for every installed runtime Debian package.

Images can remain in a private registry. Deliver this bundle with each image;
recipients do not need registry credentials to extract source from their copy:

```sh
container=$(docker create --entrypoint /bin/true YOUR_IMAGE)
docker cp "$container:/usr/share/doc/graph-engine" ./graph-engine-doc
docker rm "$container"
tar -xzf graph-engine-doc/corresponding-source.tar.gz -C ./graph-engine-doc
```

Read `REBUILD.md` inside the archive for the build procedure. The source archive
contains original dependency notices as well as the aggregated notices beside it.
It excludes the historical tracked executables and Git credentials. The archive
increases image size because it includes runtime package sources as well as the
server's dependencies.

Production CI passes the actual checked-out commit into the build, verifies source,
notices, server and igraph checksums before pushing, and checks existing images
before reusing them. An unavailable exact Debian source version fails the build;
refresh the base image/package indexes or restore that exact source in a trusted
Debian source repository. Do not substitute a newer package's source.

Local builds without `SOURCE_REVISION` record a source snapshot with per-file
hashes and a null commit, rather than claiming to represent a clean Git commit.
To check an image without executing its server, run:

```sh
sh scripts/verify_container.sh YOUR_IMAGE OPTIONAL_FULL_COMMIT_SHA
```

Keep the entire bundle when delivering an image or extracting its server binary.
Supply installation information separately if the delivery is a GPLv3 User Product
that requires it. Packaging new images does not establish matching source for
historical deliveries or the old tracked `build/graph-engined` executable.

## Separate applications

The MIT clients make gRPC calls to the Graph Engine server in a separate
process. They do not import its implementation or link to igraph. Proprietary
applications may use those MIT clients, protocol definitions, and generated
stubs while retaining their own licenses, subject to the MIT notice requirements.

Keep proprietary application code out of the server executable and C shim.
Do not copy GPL implementation code into proprietary modules or link those
modules to the server or igraph. Reassess the license boundary if components
are combined or their communication becomes equivalent to implementation
linkage; a network transport alone is not a universal legal exemption.
See the [GNU GPL FAQ on separate and combined programs](https://www.gnu.org/licenses/gpl-faq.en.html#MereAggregation).

Ordinary GPL does not impose the AGPL requirement to offer source merely
because users interact with a hosted service over a network. Delivering a
binary or container to a customer is a separate distribution question.

## Contributions and forks

Naisa controls changes to this upstream repository. This contribution policy
does not restrict the modification, redistribution, or fork rights granted
by the component licenses. See [CONTRIBUTING.md](CONTRIBUTING.md).
