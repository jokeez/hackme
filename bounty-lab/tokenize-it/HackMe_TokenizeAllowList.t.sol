// SPDX-License-Identifier: AGPL-3.0-only
// HackMe bounty-lab — Tokenize.it AllowList attribute confusion checks.
pragma solidity 0.8.23;

import "forge-std/Test.sol";
import { AllowList, TRUSTED_CURRENCY } from "../../contracts/AllowList.sol";
import "../resources/CloneCreators.sol";

contract HackMe_TokenizeAllowList_Test is Test {
    AllowList internal list;
    address internal owner = 0x6109709EcFA91A80626FF3989d68f67F5b1dd126;
    address internal forwarder = 0x9109709EcFA91A80626FF3989D68f67F5B1dD129;

    function setUp() public {
        list = createAllowList(forwarder, owner);
    }

    /// TRUSTED_CURRENCY bit alone must not imply KYC bits.
    function testFuzz_invariant_trustedCurrencyNotKYC(address user) public {
        vm.assume(user != address(0));
        vm.prank(owner);
        list.set(user, TRUSTED_CURRENCY);

        assertEq(list.map(user), TRUSTED_CURRENCY);
        assertEq(list.map(user) & 1, 0, "KYC bit must stay unset for currency-only attestation");
    }

    /// Removing user zeros attributes — no stale tier bits.
    function testFuzz_invariant_removeClearsAttributes(address user, uint256 attrs) public {
        vm.assume(user != address(0));
        attrs = bound(attrs, 1, type(uint248).max);

        vm.startPrank(owner);
        list.set(user, attrs);
        list.remove(user);
        vm.stopPrank();

        assertEq(list.map(user), 0);
    }

    /// Non-owner cannot set attributes.
    function testFuzz_invariant_onlyOwnerSets(address attacker, address user, uint256 attrs) public {
        vm.assume(attacker != owner && attacker != forwarder && attacker != address(0));
        vm.assume(user != address(0));

        vm.prank(attacker);
        vm.expectRevert();
        list.set(user, attrs);
    }
}
