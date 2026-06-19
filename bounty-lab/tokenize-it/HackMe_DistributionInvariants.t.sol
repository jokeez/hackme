// SPDX-License-Identifier: AGPL-3.0-only
// HackMe bounty-lab — Distribution double-claim, reassign timing, drain ACL.
pragma solidity 0.8.23;

import "../../lib/forge-std/src/Test.sol";
import "../../contracts/factories/TokenProxyFactory.sol";
import "../../contracts/factories/DistributionCloneFactory.sol";
import "../../contracts/Distribution.sol";
import "../resources/FakePaymentToken.sol";
import "../resources/CloneCreators.sol";

contract HackMe_DistributionInvariants_Test is Test {
    AllowList allowList;
    FakePaymentToken currency;
    Token token;
    Distribution dist;
    DistributionCloneFactory factory;

    address constant admin = 0x0109709eCFa91a80626FF3989D68f67f5b1dD120;
    address constant owner = 0x6109709EcFA91A80626FF3989d68f67F5b1dd126;
    address constant holder = 0x1109709ecFA91a80626ff3989D68f67F5B1Dd121;
    address constant provider = 0x4109709eCFa91A80626ff3989d68F67f5b1DD124;
    address constant forwarder = 0x9109709EcFA91A80626FF3989D68f67F5B1dD129;

    uint256 snapshotId;
    uint64 reassignAfter;
    uint256 constant funding = 100_000e6;
    uint256 constant pricePerToken = 100_000;

    TokenProxyFactory tokenFactory;

    function setUp() public {
        reassignAfter = uint64(block.timestamp + 60 days);

        allowList = createAllowList(forwarder, admin);
        currency = new FakePaymentToken(0, 6);
        vm.prank(admin);
        allowList.set(address(currency), TRUSTED_CURRENCY);

        IFeeSettingsV2 feeSettings = createFeeSettings(
            forwarder,
            admin,
            buildFeeTypes(0, 0, 0, admin, admin, admin)
        );
        tokenFactory = new TokenProxyFactory(address(new Token(forwarder)));
        token = Token(tokenFactory.createTokenProxy(0, forwarder, feeSettings, admin, allowList, 0, "DST", "DST"));

        vm.startPrank(admin);
        token.grantRole(token.MINTALLOWER_ROLE(), admin);
        token.mint(holder, 500e18);
        token.mint(admin, 500e18);
        snapshotId = token.createSnapshot();
        vm.stopPrank();

        factory = new DistributionCloneFactory(address(new Distribution(forwarder)));
        dist = Distribution(_deployDist());
    }

    function _deployDist() internal returns (address) {
        DistributionInitializerArguments memory args = DistributionInitializerArguments({
            owner: owner,
            token: token,
            snapshotId: snapshotId,
            currency: IERC20(address(currency)),
            pricePerToken: pricePerToken,
            reassignOrDrainAfter: reassignAfter,
            initialReassignments: new Reassignment[](0)
        });
        address predicted = factory.predictCloneAddress(bytes32("hackme-dist"), forwarder, args);
        currency.mint(provider, funding);
        vm.prank(provider);
        currency.approve(predicted, funding);
        return factory.createDistributionClone(bytes32("hackme-dist"), forwarder, provider, args, funding);
    }

    function testFuzz_invariant_doubleClaimReverts(address recipient) public {
        vm.assume(recipient != address(0));

        uint256 before = currency.balanceOf(recipient);
        vm.prank(holder);
        dist.claim(recipient, 0);

        vm.prank(holder);
        vm.expectRevert("nothing to claim");
        dist.claim(recipient, 0);

        assertGe(currency.balanceOf(recipient), before);
    }

    function testFuzz_invariant_reassignBeforeDeadlineReverts(address to, uint256 amount) public {
        vm.assume(to != address(0));
        amount = bound(amount, 1, dist.eligible(holder));

        vm.prank(owner);
        vm.expectRevert("reassignment not yet available");
        dist.reassign(holder, to, amount);
    }

    function testFuzz_invariant_nonOwnerCannotReassign(address attacker, address to, uint256 amount) public {
        vm.assume(attacker != owner && attacker != forwarder);
        vm.assume(to != address(0));
        amount = bound(amount, 1, 1e18);
        vm.warp(reassignAfter + 1);

        vm.prank(attacker);
        vm.expectRevert();
        dist.reassign(holder, to, amount);
    }

    function testFuzz_invariant_drainBeforeDeadlineReverts(address recipient) public {
        vm.assume(recipient != address(0));

        vm.prank(owner);
        vm.expectRevert("drain not yet available");
        dist.drain(recipient, IERC20(address(currency)));
    }

    function testFuzz_invariant_paidOutNeverExceedsGross(uint256) public {
        uint256 gross = (token.balanceOfAt(holder, snapshotId) * pricePerToken) / (10 ** token.decimals());
        assertLe(dist.paidOut(holder), gross);
    }
}
