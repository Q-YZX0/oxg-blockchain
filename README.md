# Oxy•gen — Native Blockchain

Oxy•gen is a native blockchain designed to run on physical nodes and mesh networks disconnected from the public Internet. Its goal is to provide reliable execution and settlement infrastructure in environments with intermittent or no connectivity, enabling community-driven, resilient, and low-infrastructure use cases.

## What was it designed for?

- Offline continuity: nodes communicate over WiFi/LoRa links in a mesh topology.
- Partition resilience: nodes can keep producing blocks locally and reconcile state once connectivity is restored.
- EVM compatibility: deploy Solidity smart contracts using the EVMone engine.
- PoS governance and security: validators and delegators secure the network via staking.

The design specification, assumptions, and threat model are described in the whitepaper. Note: the whitepaper is not in English. See the `docs/` folder for references and companion materials.

## Technology stack

- **Consensus**: CometBFT (formerly Tendermint Core)
- **Execution**: EVMone (EVM compatible)
- **Base language**: Go
- **SDK/API layer**: Node.js + TypeScript

## Repository layout

```
oxy-blockchain/
├── go/                    # Blockchain core (Go)
│   ├── consensus/         # CometBFT integration
│   ├── execution/         # EVMone integration
│   ├── storage/           # LevelDB/RocksDB
│   └── network/           # P2P over mesh
└── contracts/             # Example smart contracts
    ├── OXG.sol
    ├── GreenPool.sol
    └── DAO.sol
```

## Key features

- ✅ Operates without Internet (mesh WiFi/LoRa)
- ✅ Partition-tolerant with later reconciliation
- ✅ EVM compatibility (Solidity contracts)
- ✅ Proof of Stake (PoS) consensus

## Quickstart

### Requirements

- Go 1.21+
- Node.js 18+
- CometBFT
- EVMone

### Local build

```bash
# Core (Go)
cd go
go build
```

### Containers (Docker)

This repository includes `Dockerfile` and `docker-compose.yml` for development and testing.

```bash
# Development
docker compose up --build

# Production (example)
docker compose -f docker-compose.prod.yml up -d --build
```

## Integration with oxygen-sdk

Physical nodes use `oxygen-sdk` for:
- Mesh topology and transaction/block transport
- Validator discovery
- P2P communication between nodes

## Relation to the whitepaper

This repository implements the components described in the Oxy•gen whitepaper, including:
- Offline mesh network design and delayed synchronization
- CometBFT-based consensus mechanism
- EVM-compatible execution layer (EVMone)
- PoS governance and economics (validators/delegators)

For additional context, see the whitepaper and materials in `docs/`. 

