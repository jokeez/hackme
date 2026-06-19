// SPDX-License-Identifier: AGPL-3.0-only
// HackMe bounty-lab — TokenSwap pause, price, non-owner ACL.
pragma solidity 0.8.23;

import "../../lib/forge-std/src/Test.sol";
import "../../contracts/factories/TokenProxyFactory.sol";
import "../../contracts/factories/TokenSwapCloneFactory.sol";
import "../resources/FakePaymentToken.sol";
import "../resources/CloneCreators.sol";

contract HackMe_TokenSwapInvariants_Test is Test {
    TokenSwap tokenSwap;
    Token token;
    FakePaymentToken paymentToken;

    address constant admin = 0x0109709eCFa91a80626FF3989D68f67f5b1dD120;
    address constant buyer = 0x1109709ecFA91a80626ff3989D68f67F5B1Dd121;
    address constant owner = 0x6109709EcFA91A80626FF3989d68f67F5b1dd126;
    address constant receiver = 0x7109709eCfa91A80626Ff3989D68f67f5b1dD127;
    address constant holder = 0x8109709ecfa91a80626fF3989d68f67F5B1dD128;
    address constant forwarder = 0xa109709ecfA91A80626ff3989D68F67F5b1dD12a;

    uint8 constant decimals = 6;
    uint256 constant price = 7 * 10 ** decimals;
    uint256 constant tokenAmt = 100 ether;

    TokenProxyFactory tokenFactory;
    TokenSwapCloneFactory swapFactory;

    function setUp() public {
        paymentToken = new FakePaymentToken(10_000_000 * 10 ** decimals, decimals);
        paymentToken.transfer(buyer, 1_000_000 * 10 ** decimals);
        paymentToken.transfer(holder, 1_000_000 * 10 ** decimals);

        AllowList list = createAllowList(forwarder, owner);
        vm.prank(owner);
        list.set(address(paymentToken), TRUSTED_CURRENCY);

        IFeeSettingsV2 feeSettings = createFeeSettings(
            forwarder,
            address(this),
            buildFeeTypes(0, 0, 0, admin, admin, admin)
        );

        tokenFactory = new TokenProxyFactory(address(new Token(forwarder)));
        token = Token(tokenFactory.createTokenProxy(0, forwarder, feeSettings, admin, list, 0, "SWAP", "SWAP"));

        bytes32 mintRole = token.MINTALLOWER_ROLE();
        vm.prank(admin);
        token.grantRole(mintRole, admin);
        vm.prank(admin);
        token.mint(holder, tokenAmt);

        TokenSwapInitializerArguments memory args = TokenSwapInitializerArguments(
            owner,
            payable(receiver),
            holder,
            price,
            paymentToken,
            token
        );
        swapFactory = new TokenSwapCloneFactory(address(new TokenSwap(forwarder)));
        tokenSwap = TokenSwap(swapFactory.createTokenSwapClone(0, forwarder, args));

        vm.prank(holder);
        token.approve(address(tokenSwap), tokenAmt);
        vm.prank(buyer);
        paymentToken.approve(address(tokenSwap), type(uint256).max);
    }

    function testFuzz_invariant_buyWhenPausedReverts(uint256 amount) public {
        amount = bound(amount, 1, tokenAmt / 2);

        vm.prank(owner);
        tokenSwap.pause();

        vm.prank(buyer);
        vm.expectRevert();
        tokenSwap.buy(amount, type(uint256).max, buyer);
    }

    function testFuzz_invariant_nonOwnerCannotPause(address attacker) public {
        vm.assume(attacker != owner && attacker != forwarder);

        vm.prank(attacker);
        vm.expectRevert();
        tokenSwap.pause();
    }

    function testFuzz_invariant_buyRespectsMaxCurrency(uint256 amount) public {
        amount = bound(amount, 1, tokenAmt);
        uint256 maxPay = (amount * price) / (10 ** token.decimals());
        if (maxPay > 0) {
            maxPay -= 1;
        }

        vm.prank(buyer);
        vm.expectRevert("Purchase more expensive than _maxCurrencyAmount");
        tokenSwap.buy(amount, maxPay, buyer);
    }

    function testFuzz_invariant_soldTokensComeFromHolder(uint256 amount) public {
        amount = bound(amount, 1, tokenAmt / 4);
        uint256 holderBefore = token.balanceOf(holder);

        vm.prank(buyer);
        tokenSwap.buy(amount, type(uint256).max, buyer);

        assertEq(token.balanceOf(holder), holderBefore - amount);
    }
}
