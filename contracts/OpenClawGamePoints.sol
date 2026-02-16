// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

/// @title OpenClawGamePoints
/// @notice Non-transferable, non-redeemable game points for off-chain game settlement.
/// @dev This contract is intentionally closed-loop:
///      - Users can claim fixed daily points.
///      - Points cannot be transferred or withdrawn.
///      - Approved game operators can settle table deltas.
contract OpenClawGamePoints {
    error NotOwner();
    error NotOperator();
    error ZeroAddress();
    error AlreadyClaimed();
    error InvalidAmount();
    error BalanceTooLow();

    event OwnerTransferred(address indexed oldOwner, address indexed newOwner);
    event OperatorUpdated(address indexed operator, bool allowed);
    event DailyConfigUpdated(uint256 amount, uint256 interval);

    event DailyClaim(address indexed player, uint256 amount, uint256 claimAt);
    event MintForGame(address indexed player, uint256 amount, bytes32 indexed tableId, bytes32 indexed handId);
    event BurnForGame(address indexed player, uint256 amount, bytes32 indexed tableId, bytes32 indexed handId);
    event SettledDelta(address indexed player, int256 delta, bytes32 indexed tableId, bytes32 indexed handId);

    struct Settlement {
        address player;
        int256 delta;
    }

    string public constant name = "OpenClaw Game Point";
    string public constant symbol = "OGP";
    uint8 public constant decimals = 0;

    address public owner;

    uint256 public totalSupply;
    uint256 public dailyClaimAmount;
    uint256 public claimInterval;

    mapping(address => uint256) public balanceOf;
    mapping(address => uint256) public lastClaimAt;
    mapping(address => bool) public operators;

    modifier onlyOwner() {
        if (msg.sender != owner) revert NotOwner();
        _;
    }

    modifier onlyOperator() {
        if (!operators[msg.sender]) revert NotOperator();
        _;
    }

    constructor(address initialOwner, uint256 initialDailyAmount, uint256 initialInterval) {
        if (initialOwner == address(0)) revert ZeroAddress();
        if (initialDailyAmount == 0 || initialInterval == 0) revert InvalidAmount();

        owner = initialOwner;
        dailyClaimAmount = initialDailyAmount;
        claimInterval = initialInterval;

        emit OwnerTransferred(address(0), initialOwner);
        emit DailyConfigUpdated(initialDailyAmount, initialInterval);
    }

    function transferOwnership(address newOwner) external onlyOwner {
        if (newOwner == address(0)) revert ZeroAddress();
        address oldOwner = owner;
        owner = newOwner;
        emit OwnerTransferred(oldOwner, newOwner);
    }

    function setOperator(address operator, bool allowed) external onlyOwner {
        if (operator == address(0)) revert ZeroAddress();
        operators[operator] = allowed;
        emit OperatorUpdated(operator, allowed);
    }

    function setDailyClaimConfig(uint256 amount, uint256 interval) external onlyOwner {
        if (amount == 0 || interval == 0) revert InvalidAmount();
        dailyClaimAmount = amount;
        claimInterval = interval;
        emit DailyConfigUpdated(amount, interval);
    }

    /// @notice Claim daily points once per claim interval (e.g. once per 24h).
    function claimDaily() external {
        uint256 last = lastClaimAt[msg.sender];
        if (last != 0 && block.timestamp < last + claimInterval) {
            revert AlreadyClaimed();
        }
        lastClaimAt[msg.sender] = block.timestamp;
        _mint(msg.sender, dailyClaimAmount);
        emit DailyClaim(msg.sender, dailyClaimAmount, block.timestamp);
    }

    /// @notice Operator mints points for game rewards (e.g. tournament bonus).
    function mintForGame(address player, uint256 amount, bytes32 tableId, bytes32 handId) external onlyOperator {
        if (player == address(0)) revert ZeroAddress();
        if (amount == 0) revert InvalidAmount();
        _mint(player, amount);
        emit MintForGame(player, amount, tableId, handId);
    }

    /// @notice Operator burns points for game spend.
    function burnForGame(address player, uint256 amount, bytes32 tableId, bytes32 handId) external onlyOperator {
        if (player == address(0)) revert ZeroAddress();
        if (amount == 0) revert InvalidAmount();
        _burn(player, amount);
        emit BurnForGame(player, amount, tableId, handId);
    }

    /// @notice Batch settle one hand/session using net deltas per player.
    /// @dev delta > 0 mints; delta < 0 burns.
    function settleBatch(Settlement[] calldata settlements, bytes32 tableId, bytes32 handId) external onlyOperator {
        uint256 len = settlements.length;
        for (uint256 i = 0; i < len; i++) {
            Settlement calldata s = settlements[i];
            if (s.player == address(0)) revert ZeroAddress();

            int256 delta = s.delta;
            if (delta > 0) {
                _mint(s.player, uint256(delta));
            } else if (delta < 0) {
                _burn(s.player, uint256(-delta));
            }
            emit SettledDelta(s.player, delta, tableId, handId);
        }
    }

    /// @notice Always false because token is non-transferable by design.
    function transfer(address, uint256) external pure returns (bool) {
        revert("NON_TRANSFERABLE");
    }

    /// @notice Always false because token is non-transferable by design.
    function approve(address, uint256) external pure returns (bool) {
        revert("NON_TRANSFERABLE");
    }

    /// @notice Always false because token is non-transferable by design.
    function transferFrom(address, address, uint256) external pure returns (bool) {
        revert("NON_TRANSFERABLE");
    }

    function _mint(address to, uint256 amount) internal {
        balanceOf[to] += amount;
        totalSupply += amount;
    }

    function _burn(address from, uint256 amount) internal {
        uint256 bal = balanceOf[from];
        if (bal < amount) revert BalanceTooLow();
        unchecked {
            balanceOf[from] = bal - amount;
            totalSupply -= amount;
        }
    }
}
