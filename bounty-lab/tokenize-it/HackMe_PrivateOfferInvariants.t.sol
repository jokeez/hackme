// SPDX-License-Identifier: AGPL-3.0-only
// HackMe bounty-lab — PrivateOffer constructor guards (expiry, allowlist currency).
pragma solidity 0.8.23;

import "../../lib/forge-std/src/Test.sol";
import "../../contracts/PrivateOffer.sol";
import "../../contracts/factories/TokenProxyFactory.sol";
import "../resources/CloneCreators.sol";
import "../resources/ERC20MintableByAnyone.sol";

contract HackMe_PrivateOfferInvariants_Test is Test {
    AllowList list;
    FeeSettings feeSettings;
    Token token;
    ERC20MintableByAnyone currency;

    address constant admin = 0x0109709eCFa91a80626FF3989D68f67f5b1dD120;
    address constant buyer = 0x1109709ecFA91a80626ff3989D68f67F5B1Dd121;
    address constant owner = 0x6109709EcFA91A80626FF3989d68f67F5b1dd126;
    address constant currencyReceiver = 0x7109709eCfa91A80626Ff3989D68f67f5b1dD127;
    address constant forwarder = 0x9109709EcFA91A80626FF3989D68f67F5B1dD129;

    uint256 constant price = 10 ** 6;
    uint256 constant tokenAmount = 1e18;

    function setUp() public {
        currency = new ERC20MintableByAnyone("CUR", "CUR");
        list = createAllowList(forwarder, owner);
        vm.prank(owner);
        list.set(address(currency), TRUSTED_CURRENCY);

        feeSettings = createFeeSettings(forwarder, address(this), buildFeeTypes(0, 0, 0, admin, admin, admin));
        TokenProxyFactory tokenFactory = new TokenProxyFactory(address(new Token(forwarder)));
        token = Token(tokenFactory.createTokenProxy(0, forwarder, feeSettings, admin, list, 0, "PO", "PO"));
    }

    function _args(uint256 expiration) internal view returns (PrivateOfferArguments memory) {
        return PrivateOfferArguments(
            buyer,
            buyer,
            currencyReceiver,
            tokenAmount,
            price,
            expiration,
            IERC20(address(currency)),
            token,
            address(0)
        );
    }

    function testFuzz_invariant_expiredDealReverts(uint256 past) public {
        past = bound(past, 1, 365 days);
        vm.warp(block.timestamp + past + 1);
        vm.expectRevert("Deal expired");
        new PrivateOffer(_args(block.timestamp - 1));
    }

    function testFuzz_invariant_untrustedCurrencyReverts(address badCurrency) public {
        vm.assume(badCurrency != address(currency));
        vm.assume(badCurrency != address(0));
        vm.assume(badCurrency.code.length == 0);

        PrivateOfferArguments memory args = _args(block.timestamp + 1 days);
        args.currency = IERC20(badCurrency);

        vm.expectRevert("currency needs to be on the allowlist with TRUSTED_CURRENCY attribute");
        new PrivateOffer(args);
    }

    function testFuzz_invariant_zeroTokenAmountReverts() public {
        PrivateOfferArguments memory args = _args(block.timestamp + 1 days);
        args.tokenAmount = 0;
        vm.expectRevert("_arguments.tokenAmount can not be zero");
        new PrivateOffer(args);
    }
}
