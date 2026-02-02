require("@nomicfoundation/hardhat-ethers");

const ACCOUNTS = [
  {
    privateKey: "0x59c6995e998f97a5a0044976f7d5a5a7e8ad7b1c0c19aaea5edc9b3f6a7d6a99",
    balance: "10000000000000000000000"
  },
  {
    privateKey: "0x8b3a350cf5c34c9194ca3a545d4c1e6f9f2b0f0e2e05c1b3f2b7f0d6f7f2b4d1",
    balance: "10000000000000000000000"
  }
];

module.exports = {
  solidity: "0.8.20",
  networks: {
    hardhat: {
      chainId: 31337,
      accounts: ACCOUNTS,
      mining: {
        auto: false,
        interval: 0
      }
    },
    local: {
      url: "http://hardhat:8545",
      accounts: ACCOUNTS.map(a => a.privateKey)
    }
  }
};
