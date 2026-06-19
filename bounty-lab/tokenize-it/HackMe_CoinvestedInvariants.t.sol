// SPDX-License-Identifier: AGPL-3.0-only
// HackMe bounty-lab — CoinvestedPosition timelock, carry settlement, ACL.
pragma solidity 0.8.23;

import "../../lib/forge-std/src/Test.sol";
import "../../contracts/factories/TokenProxyFactory.sol";
import "../../contracts/factories/CoinvestedPositionCloneFactory.sol";
import "../../contracts/GlobalTokenExitRegistry.sol";
import "../resources/FakePaymentToken.sol";
import "../resources/CloneCreators.sol";

contract HackMe_CoinvestedInvariants_Test is Test {
    Token token;
    FakePaymentToken eurc;
    CoinvestedPosition position;
    GlobalTokenExitRegistry registry;

    address constant admin = 0x0109709eCFa91a80626FF3989D68f67f5b1dD120;
    address constant owner = 0x6109709EcFA91A80626FF3989d68f67F5b1dd126;
    address constant receiver = 0x7109709eCfa91A80626Ff3989D68f67f5b1dD127;
    address constant leadA = 0x2109709EcFa91a80626Ff3989d68F67F5B1Dd122;
    address constant buyer = 0x1109709ecFA91a80626ff3989D68f67F5B1Dd121;
    address constant forwarder = 0x9109709EcFA91A80626FF3989D68f67F5B1dD129;

    uint64 constant CARRY_HALF = uint64(type(uint64).max / 2);

    function setUp() public {
        AllowList list = createAllowList(forwarder, admin);
        eurc = new FakePaymentToken(10_000_000e6, 6);
        vm.prank(admin);
        list.set(address(eurc), TRUSTED_CURRENCY);

        IFeeSettingsV2 feeSettings = createFeeSettings(
            forwarder,
            admin,
            buildFeeTypes(0, 0, 0, admin, admin, admin)
        );

        TokenProxyFactory tokenFactory = new TokenProxyFactory(address(new Token(forwarder)));
        token = Token(tokenFactory.createTokenProxy(0, forwarder, feeSettings, admin, list, 0, "CIP", "CIP"));

        bytes32 mintRole = token.MINTALLOWER_ROLE();
        vm.startPrank(admin);
        token.grantRole(mintRole, admin);
        vm.stopPrank();

        registry = new GlobalTokenExitRegistry(forwarder);

        LeadInvestor[] memory leads = new LeadInvestor[](1);
        leads[0] = LeadInvestor({account: leadA, carryFraction: CARRY_HALF});

        uint64 lock = uint64(block.timestamp + 30 days);
        CoinvestedPositionInitializerArguments memory args = CoinvestedPositionInitializerArguments({
            owner: owner,
            receiver: receiver,
            leadInvestors: leads,
            basePrice: 100e6,
            baseCurrency: IERC20(address(eurc)),
            token: token,
            lockedUntil: lock,
            tokenExitRegistry: registry
        });

        CoinvestedPositionCloneFactory factory =
            new CoinvestedPositionCloneFactory(address(new CoinvestedPosition(forwarder)));
        position = CoinvestedPosition(factory.createCoinvestedPositionClone(bytes32("hackme-cip"), forwarder, args));

        vm.prank(admin);
        token.mint(address(position), 1_000_000 ether);

        vm.prank(owner);
        position.setTokenPrice(200e6);
    }

    function testFuzz_invariant_unpauseBeforeTimelockReverts(uint256 warp) public {
        warp = bound(warp, 0, 29 days);
        vm.warp(block.timestamp + warp);

        vm.prank(owner);
        vm.expectRevert("timelock has not expired");
        position.unpause();
    }

    function testFuzz_invariant_nonOwnerCannotUnpause(address attacker) public {
        vm.warp(block.timestamp + 31 days);
        vm.assume(attacker != owner && attacker != forwarder);

        vm.prank(attacker);
        vm.expectRevert();
        position.unpause();
    }

    function testFuzz_invariant_buyDistributesNoMoreThanPaid(uint256 amount) public {
        amount = bound(amount, 1 ether, 10_000 ether);
        vm.warp(block.timestamp + 31 days);
        vm.prank(owner);
        position.unpause();

        eurc.mint(buyer, 10_000_000e6);
        uint256 buyerBefore = eurc.balanceOf(buyer);
        uint256 leadBefore = eurc.balanceOf(leadA);
        uint256 recvBefore = eurc.balanceOf(receiver);

        vm.startPrank(buyer);
        eurc.approve(address(position), type(uint256).max);
        position.buy(amount, type(uint256).max, buyer);
        vm.stopPrank();

        uint256 paid = buyerBefore - eurc.balanceOf(buyer);
        assertGt(paid, 0);
        assertLe(eurc.balanceOf(leadA) + eurc.balanceOf(receiver) - leadBefore - recvBefore, paid);
    }

    function testFuzz_invariant_setCurrencyBeforeTimelockReverts(address bad) public {
        vm.assume(bad != address(eurc));
        vm.prank(owner);
        vm.expectRevert("timelock has not expired");
        position.setCurrency(IERC20(bad), 1);
    }
}
