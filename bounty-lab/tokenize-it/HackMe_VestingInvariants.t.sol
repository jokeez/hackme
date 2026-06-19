// SPDX-License-Identifier: AGPL-3.0-only
// HackMe bounty-lab — Vesting release caps, ACL, commitment reveal.
pragma solidity 0.8.23;

import "../../lib/forge-std/src/Test.sol";
import "../../contracts/factories/VestingCloneFactory.sol";
import "../resources/ERC20MintableByAnyone.sol";

contract HackMe_VestingInvariants_Test is Test {
    Vesting vesting;
    ERC20MintableByAnyone token;

    address constant owner = 0x6109709EcFA91A80626FF3989d68f67F5b1dd126;
    address constant beneficiary = 0x1109709ecFA91a80626ff3989D68f67F5B1Dd121;
    address constant forwarder = 0x9109709EcFA91A80626FF3989D68f67F5B1dD129;

    uint256 constant allocation = 1_000_000 ether;
    uint64 constant cliffDur = 30 days;
    uint64 constant vestDur = 365 days;

    function setUp() public {
        token = new ERC20MintableByAnyone("VEST", "VEST");
        Vesting logic = new Vesting(forwarder);
        VestingCloneFactory factory = new VestingCloneFactory(address(logic));
        vesting = Vesting(factory.createVestingClone(0, forwarder, owner, address(token)));
    }

    function _createMintableVesting() internal returns (uint64 id) {
        uint64 start = uint64(block.timestamp);
        vm.prank(owner);
        id = vesting.createVesting(allocation, beneficiary, start, cliffDur, vestDur, true);
    }

    function testFuzz_invariant_releasedNeverExceedsAllocation(uint256 warpDays) public {
        uint64 id = _createMintableVesting();
        warpDays = bound(warpDays, cliffDur / 1 days, (vestDur / 1 days) + 30);

        vm.warp(block.timestamp + warpDays * 1 days);
        vm.prank(beneficiary);
        vesting.release(id);

        assertLe(vesting.released(id), vesting.allocation(id));
        assertLe(token.balanceOf(beneficiary), vesting.allocation(id));
    }

    function testFuzz_invariant_nonBeneficiaryCannotRelease(address thief) public {
        uint64 id = _createMintableVesting();
        vm.warp(block.timestamp + cliffDur + 1 days);
        vm.assume(thief != beneficiary && thief != forwarder && thief != address(0));

        vm.prank(thief);
        vm.expectRevert("Only beneficiary can release tokens");
        vesting.release(id);
    }

    function testFuzz_invariant_nonManagerCannotCommit(address attacker, bytes32 hash) public {
        vm.assume(attacker != owner && attacker != forwarder);
        vm.assume(hash != bytes32(0));

        vm.prank(attacker);
        vm.expectRevert("Caller is not a manager");
        vesting.commit(hash);
    }

    function testFuzz_invariant_revealHashMustMatch(
        uint256 alloc,
        uint64 start,
        uint64 cliff,
        uint64 duration,
        bytes32 salt
    ) public {
        alloc = bound(alloc, 1, 1e30);
        start = uint64(bound(start, block.timestamp + 1, block.timestamp + 365 days));
        cliff = uint64(bound(cliff, 1 days, 90 days));
        duration = uint64(bound(duration, cliff, 400 days));

        bytes32 hash = keccak256(abi.encodePacked(alloc, beneficiary, start, cliff, duration, true, salt));
        vm.prank(owner);
        vesting.commit(hash);

        vm.prank(owner);
        vm.expectRevert("invalid-hash");
        vesting.reveal(hash, alloc + 1, beneficiary, start, cliff, duration, true, salt);
    }

    function testFuzz_invariant_ownerCannotChangeBeneficiaryEarly(address newBen) public {
        uint64 id = _createMintableVesting();
        vm.assume(newBen != address(0) && newBen != beneficiary);

        vm.prank(owner);
        vm.expectRevert("Only beneficiary can change beneficiary, or owner 1 year after vesting end");
        vesting.changeBeneficiary(id, newBen);
    }
}
