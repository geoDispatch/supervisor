# Contracts: synced copies (do not edit)

The JSON files in this directory are byte-identical copies of the canonical
GeoDispatch contracts (contract version 2):

| File | Canonical source |
|---|---|
| `ai_request.json` | `contracts/examples/ai_request.json` |
| `ai_response.json` | `contracts/examples/ai_response.json` |
| `camara_device.json` | `contracts/examples/camara_device.json` |
| `sensor_input.json` | `contracts/examples/sensor_input.json` |
| `ws_update.json` | `contracts/examples/ws_update.json` |

The canonical source is the `contracts` repository: the `contracts/` directory
next to `supervisor/` (in the deploy layout, the `contracts/` submodule). The
contracts, the WebSocket v2 flow and the migration notes are documented in
`contracts/docs/README.md` and `contracts/CHANGELOG.md`.

To change a contract, edit the canonical file and run `sh contracts/sync.sh`.
`sh contracts/sync.sh --check` compares every checked-out service copy with
the canonical files and exits 1 when any copy has drifted.
