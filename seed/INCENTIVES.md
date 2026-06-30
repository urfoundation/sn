# UR BitTensor subnet incentives and design

We are building a new BitTensor subnet to run a decentralized privacy network with the components below.


## Explanation of components

Block size: 7 days
Subnet contract accumulates payments and emissions over the course of the block, to be distributed at the end of the block.
The payout from the subnet contract is weighted by the payment of the network operator over the total payments, and then distributes the balance according to each network operator's distribution.

Network operator: runs the servers. Each network operator has a mining slot and a validator slot on the BitTensor subnet, which are pooled rewards for their attached miners and validators. The network operator determines the payout from their mining slots. The network operator pays into the subnet contract per used GB and active user (per month) based on the global fixed rate. 
Miner: connects to one or more network operators simultaneously. Providers provide egress and ingress traffic. Miners accumulate contracts, issued by the network operator. Miners accumulate reliability, issues by the network operator.
Validator: connects to one or more network operators. Follows the validation protocol described in VALIDATOR.md. Accumulates signed validated paths from the network operator.

The only mechanism that keeps the components coordinating are the incentives below.


## Incentive Direction

Explanation of ST contract approach for miners:
- NO deposits into ST contract per GB and per user. The rate is set by an oracle. Deposit transaction DT
- NO publishes their own payout list ever block into an oracle
- NO publishes list of their deposits and signs with wallet
- The SUM(DT) for each NO weights the payout from balance+emissions for the NO payout list
Explanation of miner incentives: the NO will want to maximize their deposits, to encourage more providers on their network. The cost of deposits will be constrained by the revenue of the NO, which reflects real usage (data and active users). NOs compete with each other for providers. Providers will only join NOs that are profitable.

Explanation of ST contract approach for validators:
- V deposits into ST for each validated path a validation fee. Validation transaction VT
- NO publishes validated paths into an oracle. V uses their wallet PK as their validation path key
- SUM(DT) weights the emissions payout for the NO validators list. The NO and the V split INTERSECTION(VT,NO)/UNION(VT,NO) of the payout. The rest goes to the owners (e.g. disagreement goes to the subnet owners)
Explanation of validator incentives: prisoner's dilemma: the NO and V will get the most outcome by honestly reporting. The cost of V misreporting is the deposit cost. The cost of the NO misreporting is lowered rewards. The owners want healthy validators to improve the product quality. The rational measure of fake V is how much NOs mistrust each other, or how much the owners mistrust the NOs.

Open questions:
- How is oracle data stored and charged on SubTensor? Can we adapt the network operator payout table to use a Merkle tree so that each miner can validate their payout without having to store it on chain?
- Are smart contracts standard EVM?
- How to adapt this to standard BT payout formulas?


## Design goals

We want to build BitTensor smart contract and emission schedule to run this subnet. It should follow the best practice and payout formulas established by the BitTensor community.

Spend extra time on deep research so that we capture the current state of BitTensor accurately.

Create a WHITEPAPER.md that desribes in detail enough that it can be clearly implemented how this subnet will work.
