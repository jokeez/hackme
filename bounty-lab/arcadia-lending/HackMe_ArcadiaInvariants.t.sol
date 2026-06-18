// SPDX-License-Identifier: BUSL-1.1
// HackMe bounty-lab — custom invariants (not upstream fuzz reruns).
pragma solidity ^0.8.0;

import { LendingPool_Fuzz_Test } from "../fuzz/LendingPool/_LendingPool.fuzz.t.sol";
import { LiquidatorErrors } from "../../../src/libraries/Errors.sol";
import { LendingPoolErrors } from "../../../src/libraries/Errors.sol";

/// @notice Extra invariants: access control + liquidation preconditions.
contract HackMe_ArcadiaInvariants_Test is LendingPool_Fuzz_Test {
    function setUp() public override {
        LendingPool_Fuzz_Test.setUp();
    }

    /// Healthy account (collateral >> debt) must not start liquidation.
    function testFuzz_invariant_healthyNotLiquidatable(
        uint112 collateral,
        uint112 loan,
        address attacker
    ) public {
        vm.assume(loan > 1000 && loan < type(uint112).max / 4);
        vm.assume(collateral > loan * 3 && collateral < type(uint112).max);
        bytes3 empty;
        depositErc20InAccount(account, mockERC20.stable1, collateral);
        vm.prank(users.liquidityProvider);
        mockERC20.stable1.approve(address(pool), type(uint256).max);
        vm.prank(address(srTranche));
        pool.depositInLendingPool(loan, users.liquidityProvider);
        vm.prank(users.accountOwner);
        pool.borrow(loan, address(account), users.accountOwner, empty);

        vm.prank(attacker);
        vm.expectRevert();
        liquidator.liquidateAccount(address(account));
    }

    /// Random address cannot borrow on behalf of account without approval.
    function testFuzz_invariant_randomBorrowerReverts(uint256 amount, address thief, address to) public {
        vm.assume(amount > 0 && amount < type(uint112).max);
        vm.assume(thief != users.accountOwner && thief != address(0));

        vm.prank(thief);
        vm.expectRevert();
        pool.borrow(amount, address(account), to, emptyBytes3);
    }

    /// Cannot borrow zero — invariant on all callers.
    function testFuzz_invariant_zeroBorrowAlwaysReverts(address caller, address to) public {
        vm.prank(caller);
        vm.expectRevert(LendingPoolErrors.ZeroAmount.selector);
        pool.borrow(0, address(account), to, emptyBytes3);
    }
}
