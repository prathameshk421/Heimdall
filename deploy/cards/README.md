# Personal context cards

The folders under `examples/` are samples of the format, not data: copy the shape, write your own.

Layout: `<cards_dir>/<person>/<file>`, where `<person>` is the key under `people:` in the repository's `heimdall.yaml`
and the reviewer agent is mapped to that person through the same file. Cards are `.md`, `.yaml`, `.yml` or `.txt`,
at most 64 KiB each, directly in the person's folder. Keep them short and factual: what you own, the rules that must
hold in your code, incidents and what they taught, how your team writes code, what you do not know.

Turn the tool on with `cards_dir = "/var/lib/heimdall/cards"` in `heimdall.toml` (an absolute path inside the
server container; that folder is on the durable `heimdall_data` volume). Load cards without a rebuild:

    docker compose cp deploy/cards/examples/. heimdall-server:/var/lib/heimdall/cards/

The reviewer calls `read_card` with no file to list its own cards, then with a name to read one. The server picks
the person from the authenticated agent. A reviewer cannot name another person or leave the folder. Nothing in a card
is trusted as an instruction: it reaches the model wrapped as untrusted data. Do not put secrets in cards.
