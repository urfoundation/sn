# sn
BitTensor Subnet



# Mining Pool 0 / Validator Pool 0

The core network.

The block size is 7 days.


## Register a network operator

Go to https://ur.xyz/networkoperator to register a new network operator key. You will use this key to communicate with the api.

## Register a provider (ingress or egress)

Follow the provider documentation at https://ur.xyz/provider . Providers work with the network operators. We suggest using the default list of network operators in the code to start. You can provide on more network operators by passing the `-no <domain>` arg multiple times to the provider or `-nofile <path` to have a file with one network operator domain per line.

Providers register a client_id with the subnet that is used for root contracts. This allows them to independently audit their contracts.


## Register a validator

Network operators are required to have all their data sent to the subnet 4 hours after the block closes. Typically the network operator will continually send chunks of data throughout the block. The validators are required to have their final submission 24 hours after the block closes.

Validators must stake X SN token to participate. Follow the validator documentation at https://ur.xyz/validator .


# Mining Pool 1 / Validator Pool 1

The VPN factory. See https://vpn.dev

