// SPDX-License-Identifier: AGPL-3.0-only
// HackMe bounty-lab — Crowdinvesting access control + cap invariants.
pragma solidity 0.8.23;

import "../../lib/forge-std/src/Test.sol";
import "../../contracts/factories/TokenProxyFactory.sol";
import "../../contracts/factories/CrowdinvestingCloneFactory.sol";
import "../resources/FakePaymentToken.sol";
import "../resources/CloneCreators.sol";

contract HackMe_CrowdinvestingInvariants_Test is Test {
    Crowdinvesting crowdinvesting;
    AllowList list;
    IFeeSettingsV2 feeSettings;
    Token token;
    FakePaymentToken paymentToken;

    address constant admin = 0x0109709eCFa91a80626FF3989D68f67f5b1dD120;
    address constant owner = 0x6109709EcFA91A80626FF3989d68f67F5b1dd126;
    address constant buyer = 0x1109709ecFA91a80626ff3989D68f67F5B1Dd121;
    address constant receiver = 0x7109709eCfa91A80626Ff3989D68f67f5b1dD127;
    address constant forwarder = 0x9109709EcFA91A80626FF3989D68f67F5B1dD129;

    uint8 constant paymentDecimals = 6;
    uint256 constant price = 7 * 10 ** paymentDecimals;
    uint256 constant maxSold = 20 * 10 ** 18;
    uint256 constant maxPerBuyer = maxSold / 2;
    uint256 constant minPerBuyer = maxSold / 200;

    function setUp() public {
        paymentToken = new FakePaymentToken(1_000_000 * 10 ** paymentDecimals, paymentDecimals);
        paymentToken.transfer(buyer, 1_000_000 * 10 ** paymentDecimals);

        list = createAllowList(forwarder, owner);
        vm.prank(owner);
        list.set(address(paymentToken), TRUSTED_CURRENCY);

        feeSettings = createFeeSettings(
            forwarder,
            address(this),
            buildFeeTypes(0, 0, 0, admin, admin, admin)
        );

        TokenProxyFactory tokenFactory = new TokenProxyFactory(address(new Token(forwarder)));
        token = Token(
            tokenFactory.createTokenProxy(0, forwarder, feeSettings, admin, list, 0, "HM", "HM")
        );

        vm.prank(owner);
        CrowdinvestingCloneFactory factory =
            new CrowdinvestingCloneFactory(address(new Crowdinvesting(forwarder)));

        CrowdinvestingInitializerArguments memory args = CrowdinvestingInitializerArguments(
            owner,
            payable(receiver),
            minPerBuyer,
            maxPerBuyer,
            price,
            price,
            price,
            maxSold,
            paymentToken,
            token,
            0,
            address(0),
            address(0)
        );
        crowdinvesting = Crowdinvesting(factory.createCrowdinvestingClone(0, forwarder, args));

        vm.prank(admin);
        token.increaseMintingAllowance(address(crowdinvesting), maxSold);

        vm.prank(buyer);
        paymentToken.approve(address(crowdinvesting), type(uint256).max);
    }

    function testFuzz_invariant_onlyOwnerPauses(address attacker) public {
        vm.assume(attacker != owner && attacker != forwarder);
        vm.prank(attacker);
        vm.expectRevert();
        crowdinvesting.pause();
    }

    function testFuzz_invariant_buyWhenPausedReverts(uint256 amount) public {
        amount = bound(amount, minPerBuyer, maxPerBuyer);
        vm.prank(owner);
        crowdinvesting.pause();

        vm.prank(buyer);
        vm.expectRevert();
        crowdinvesting.buy(amount, type(uint256).max, buyer);
    }

    function testFuzz_invariant_tokensSoldNeverExceedsCap(uint256 amount) public {
        amount = bound(amount, minPerBuyer, maxPerBuyer);

        vm.prank(buyer);
        crowdinvesting.buy(amount, type(uint256).max, buyer);

        assertLe(crowdinvesting.tokensSold(), crowdinvesting.maxAmountOfTokenToBeSold());
    }

    function testFuzz_invariant_cumulativeBuyNeverExceedsPerBuyerCap(uint256 a, uint256 b) public {
        a = bound(a, minPerBuyer, maxPerBuyer);
        b = bound(b, minPerBuyer, maxPerBuyer);
        vm.assume(a + b <= maxPerBuyer);

        vm.startPrank(buyer);
        crowdinvesting.buy(a, type(uint256).max, buyer);
        crowdinvesting.buy(b, type(uint256).max, buyer);
        vm.stopPrank();

        assertLe(crowdinvesting.tokensSold(), crowdinvesting.maxAmountOfTokenToBeSold());
        assertLe(a + b, maxPerBuyer);
    }

    function testFuzz_invariant_nonOwnerCannotChangeReceiver(address attacker, address newRecv) public {
        vm.assume(attacker != owner && attacker != forwarder);
        vm.assume(newRecv != address(0));

        vm.prank(owner);
        crowdinvesting.pause();

        vm.prank(attacker);
        vm.expectRevert();
        crowdinvesting.setCurrencyReceiver(newRecv);
    }
}
