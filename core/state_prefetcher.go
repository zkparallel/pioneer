// Copyright 2019 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package core

import (
	"github.com/ethereum/go-ethereum/consensus/misc"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
)

// statePrefetcher is a basic Prefetcher, which blindly executes a block on top
// of an arbitrary state with the goal of prefetching potentially useful state
// data from disk before the main block processor start executing.
type statePrefetcher struct {
	config *params.ChainConfig // Chain configuration options
	bc     *BlockChain         // Canonical block chain
	engine consensus.Engine    // Consensus engine used for block rewards
}

// newStatePrefetcher initialises a new statePrefetcher.
func newStatePrefetcher(config *params.ChainConfig, bc *BlockChain, engine consensus.Engine) *statePrefetcher {
	return &statePrefetcher{
		config: config,
		bc:     bc,
		engine: engine,
	}
}

// Prefetch processes the state changes according to the Ethereum rules by running
// the transaction messages using the statedb, but any changes are discarded. The
// only goal is to pre-cache transaction signatures and state trie nodes.
func (p *statePrefetcher) Prefetch(block *types.Block, statedb *state.StateDB, cfg vm.Config, interrupt *atomic.Bool) {
	//var (
	//	header       = block.Header()
	//	gaspool      = new(GasPool).AddGas(block.GasLimit())
	//	blockContext = NewEVMBlockContext(header, p.bc, nil)
	//	evm          = vm.NewEVM(blockContext, vm.TxContext{}, statedb, p.config, cfg)
	//	signer       = types.MakeSigner(p.config, header.Number, header.Time)
	//)
	//// Iterate over and process the individual transactions
	//byzantium := p.config.IsByzantium(block.Number())
	//if len(block.Transactions()) > 0 {
	//	types.InitTxFile(header.Number)
	//}
	//for i, tx := range block.Transactions() {
	//	// If block precaching was interrupted, abort
	//	if interrupt != nil && interrupt.Load() {
	//		return
	//	}
	//	// Convert the transaction into an executable message and pre-cache its sender
	//	msg, err := TransactionToMessage(tx, signer, header.BaseFee)
	//	if err != nil {
	//		return // Also invalid block, bail out
	//	}
	//	statedb.SetTxContext(tx.Hash(), i)
	//	types.WriteHash(header.Number, tx.Hash())
	//	if err := precacheTransaction(msg, p.config, gaspool, statedb, header, evm); err != nil {
	//		log.Error("precacheTransaction", "blockNumber", block.Number(), "hash", tx.Hash(), "err", err)
	//		types.DelTxFile(header.Number)
	//		return // Ugh, something went horribly wrong, bail out
	//	}
	//	types.WriteHash(header.Number, tx.Hash())
	//	// If we're pre-byzantium, pre-load trie nodes for the intermediate root
	//	if !byzantium {
	//		statedb.IntermediateRoot(true)
	//	}
	//}
	//if len(block.Transactions()) > 0 {
	//	types.ReNameTxFile(header.Number)
	//}
	//// If were post-byzantium, pre-load trie nodes for the final root hash
	//if byzantium {
	//	statedb.IntermediateRoot(true)
	//}

	var (
		usedGas     = new(uint64)
		header      = block.Header()
		blockHash   = block.Hash()
		blockNumber = block.Number()
		gp          = new(GasPool).AddGas(block.GasLimit())
	)
	// Mutate the block and state according to any hard-fork specs
	if p.config.DAOForkSupport && p.config.DAOForkBlock != nil && p.config.DAOForkBlock.Cmp(block.Number()) == 0 {
		misc.ApplyDAOHardFork(statedb)
	}
	var (
		context = NewEVMBlockContext(header, p.bc, nil)
		vmenv   = vm.NewEVM(context, vm.TxContext{}, statedb, p.config, cfg)
		signer  = types.MakeSigner(p.config, header.Number, header.Time)
	)
	if len(block.Transactions()) > 0 {
		types.InitTxFile(header.Number)
	}
	// Iterate over and process the individual transactions
	for i, tx := range block.Transactions() {
		msg, err := TransactionToMessage(tx, signer, header.BaseFee)
		if err != nil {
			types.DelTxFile(header.Number)
			return
		}
		statedb.SetTxContext(tx.Hash(), i)
		types.WriteHash(header.Number, tx.Hash())
		_, err = applyTransaction(msg, p.config, gp, statedb, blockNumber, blockHash, tx, usedGas, vmenv)
		if err != nil {
			types.DelTxFile(header.Number)
			return
		}
		types.WriteHash(header.Number, tx.Hash())
	}
	if len(block.Transactions()) > 0 {
		types.ReNameTxFile(header.Number)
	}
}

// precacheTransaction attempts to apply a transaction to the given state database
// and uses the input parameters for its environment. The goal is not to execute
// the transaction successfully, rather to warm up touched data slots.
func precacheTransaction(msg *Message, config *params.ChainConfig, gaspool *GasPool, statedb *state.StateDB, header *types.Header, evm *vm.EVM) error {
	// Update the evm with the new transaction context.
	evm.Reset(NewEVMTxContext(msg), statedb)
	// Add addresses to access list if applicable
	_, err := ApplyMessage(evm, msg, gaspool)
	return err
}
