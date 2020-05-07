/*
 * Copyright (C) 2017 The "MysteriumNetwork/node" Authors.
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

package main

import (
	"context"
	"flag"
	"fmt"
	"math/big"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/mysteriumnetwork/payments/bindings"
	"github.com/mysteriumnetwork/payments/crypto"
)

func main() {
	keyStoreDir := flag.String("keystore.directory", "", "Directory of keystore")
	etherAddress := flag.String("ether.address", "", "Account inside keystore to use for deployment")
	etherPassword := flag.String("ether.passphrase", "", "key of the account")
	ethRPC := flag.String("geth.url", "", "RPC url of ethereum client")
	flag.Parse()

	addr := common.HexToAddress(*etherAddress)
	ks := keystore.NewKeyStore(*keyStoreDir, keystore.StandardScryptN, keystore.StandardScryptP)
	acc, err := ks.Find(accounts.Account{Address: addr})
	checkError("find account", err)

	err = ks.Unlock(acc, *etherPassword)
	checkError("unlock account", err)

	client, err := ethclient.Dial(*ethRPC)
	checkError("lookup backend", err)

	chainID, err := client.NetworkID(context.Background())
	checkError("lookup chainid", err)

	transactor := &bind.TransactOpts{
		From: addr,
		Signer: func(signer types.Signer, address common.Address, tx *types.Transaction) (*types.Transaction, error) {
			return ks.SignTx(acc, tx, chainID)
		},
		Context:  context.Background(),
		GasLimit: 6721975,
	}

	deployPaymentsv2Contracts(transactor, client)
}

func deployPaymentsv2Contracts(transactor *bind.TransactOpts, client *ethclient.Client) {
	time.Sleep(time.Second * 3)

	transactor.Nonce = lookupLastNonce(transactor.From, client)
	mathSafeLibAddress, tx, _, err := bindings.DeploySafeMathLib(transactor, client)
	checkError("Deploy migrations", err)
	checkTxStatus(client, tx)
	fmt.Println("v2 mathSafeLib address:", mathSafeLibAddress.Hex())

	transactor.Nonce = lookupLastNonce(transactor.From, client)
	mystTokenAddress, tx, _, err := bindings.DeployLinkedMystToken(transactor, client, map[string]common.Address{"SafeMathLib": mathSafeLibAddress})
	checkError("Deploy token v2", err)
	checkTxStatus(client, tx)
	fmt.Println("v2 token address: ", mystTokenAddress.String())

	transactor.Nonce = lookupLastNonce(transactor.From, client)
	mystDexAddress, tx, _, err := bindings.DeployMystDEX(transactor, client)
	checkError("Deploy mystDex v2", err)
	checkTxStatus(client, tx)
	fmt.Println("v2 mystDEX address: ", mystDexAddress.String())

	transactor.Nonce = lookupLastNonce(transactor.From, client)
	channelImplAddress, tx, _, err := bindings.DeployChannelImplementation(transactor, client)
	checkError("Deploy channel impl v2", err)
	checkTxStatus(client, tx)
	fmt.Println("v2 channel impl address: ", channelImplAddress.String())

	transactor.Nonce = lookupLastNonce(transactor.From, client)
	accountantImplAddress, tx, _, err := bindings.DeployAccountantImplementation(transactor, client)
	checkError("Deploy accountant impl v2", err)
	checkTxStatus(client, tx)
	fmt.Println("v2 accountant impl address: ", accountantImplAddress.String())

	transactor.Nonce = lookupLastNonce(transactor.From, client)
	registrationFee := big.NewInt(0)
	minimalStake := big.NewInt(0)
	registryAddress, tx, _, err := bindings.DeployRegistry(
		transactor,
		client,
		mystTokenAddress,
		mystDexAddress,
		common.HexToAddress("0x46e9742C098267122DA466d6b7a3fb844436Ac37"),
		registrationFee,
		minimalStake,
	)
	checkError("Deploy registry v2", err)
	fmt.Println("v2 registry address: ", registryAddress.String())
	checkTxStatus(client, tx)

	configAddress, err := crypto.DeployConfigContract("45bb96530f3d1972fdcd2005c1987a371d0b6d378b77561c6beeaca27498f46b", client)
	checkError("Deploy config v2", err)
	fmt.Println("v2 config address:", configAddress.Hex())

	transactor.Nonce = lookupLastNonce(transactor.From, client)
	channelImplProxyAddress, _, _, err := bindings.DeployChannelImplementationProxy(transactor, client)
	checkError("Deploy channel impl proxy v2", err)
	fmt.Println("v2 channel impl proxy address: ", channelImplProxyAddress.String())

	transactor.Nonce = lookupLastNonce(transactor.From, client)
	accountantImplProxyAddress, _, _, err := bindings.DeployAccountantImplementationProxy(transactor, client)
	checkError("Deploy accountant impl proxy v2", err)
	fmt.Println("v2 accountant impl proxy address: ", accountantImplProxyAddress.String())

	transactor.Nonce = nil
	err = crypto.SetupConfig(transactor, client, transactor.From.Hex(), channelImplProxyAddress, channelImplAddress, accountantImplAddress, accountantImplProxyAddress)
	checkError("Setup config v2", err)

	ts, err := bindings.NewMystTokenTransactor(mystTokenAddress, client)
	checkError("myst transactor", err)

	transactor.Nonce = lookupLastNonce(transactor.From, client)
	tx, err = ts.Mint(transactor, transactor.From, big.NewInt(125000000000000000))
	checkError("mint myst", err)
	checkTxStatus(client, tx)

	transactor.Nonce = lookupLastNonce(transactor.From, client)
	tx, err = ts.Approve(transactor, registryAddress, big.NewInt(100000000000))
	checkError("allow myst", err)
	checkTxStatus(client, tx)

	rt, err := bindings.NewRegistryTransactor(registryAddress, client)
	checkError("registry transactor", err)

	transactor.Nonce = lookupLastNonce(transactor.From, client)
	tx, err = rt.RegisterAccountant(
		transactor,
		transactor.From,
		big.NewInt(100000000000),
		400,
		big.NewInt(125000000000),
	)
	checkError("register accountant", err)
	checkTxStatus(client, tx)

	rc, err := bindings.NewRegistryCaller(registryAddress, client)
	checkError("registry caller", err)

	accs, err := rc.GetAccountantAddress(&bind.CallOpts{
		Context: context.Background(),
		From:    transactor.From,
	}, transactor.From)
	checkError("get accountant address", err)
	fmt.Println("registered accountant", accs.Hex())

	transactor.Nonce = lookupLastNonce(transactor.From, client)
	tx, err = ts.Mint(transactor, accs, big.NewInt(125000000000000000))
	checkError("mint myst for accountant", err)
	checkTxStatus(client, tx)
}

func checkError(context string, err error) {
	if err != nil {
		fmt.Println("Error at:", context, "value:", err.Error())
		os.Exit(1)
	}
}
func lookupLastNonce(addr common.Address, client *ethclient.Client) *big.Int {
	nonce, err := client.NonceAt(context.Background(), addr, nil)
	checkError("Lookup last nonce", err)
	return big.NewInt(int64(nonce))
}

func checkTxStatus(client *ethclient.Client, tx *types.Transaction) {
	//wait for transaction to be mined at most 10 seconds
	for i := 0; i < 10; i++ {
		_, pending, err := client.TransactionByHash(context.Background(), tx.Hash())
		checkError("Get tx by hash", err)
		if pending {
			time.Sleep(1 * time.Second)
		} else {
			break
		}
	}

	receipt, err := client.TransactionReceipt(context.Background(), tx.Hash())
	checkError("Fetch tx receipt", err)
	if receipt.Status != 1 {
		fmt.Println("Receipt status expected to be 1")
		os.Exit(1)
	}
}
