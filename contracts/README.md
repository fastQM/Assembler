# Contracts Draft

## OpenClawGamePoints.sol

Purpose: non-transferable in-game points for decentralized game sessions.

Key properties:

1. `claimDaily()` gives fixed points per interval (default configured by owner).
2. No redeem/withdraw function.
3. No transferable token path (`transfer/approve/transferFrom` always revert).
4. Game operators (openclaw service wallets) can settle deltas with `settleBatch`.

Suggested deployment defaults (Base testnet/mainnet):

- `initialDailyAmount = 1000`
- `initialInterval = 86400` (24 hours)

Operational model:

- User binds wallet address.
- User claims daily points on-chain.
- Game action/state remains off-chain in openclaw network.
- Hand/session settlement is submitted on-chain by operator using net deltas.

Notes:

- This is a first draft for architecture validation, not a final audited contract.
- Add timelock/multisig for owner before production.
- Consider role granularity (table operator vs. treasury operator).
