const { ethers, network } = require("hardhat");
const fs = require("fs");

async function main() {
  const provider = ethers.provider;
  const [wallet0, wallet1] = await ethers.getSigners();

  await network.provider.send("evm_setAutomine", [false]);
  await network.provider.send("evm_setIntervalMining", [0]);

  const Token = await ethers.getContractFactory("TestToken");
  const token = await Token.deploy();
  await token.waitForDeployment();
  const tokenAddr = await token.getAddress();
  console.log("token", tokenAddr);
  try {
    fs.writeFileSync("/data/token.json", JSON.stringify({ address: tokenAddr, deployed_at: Date.now() }, null, 2));
  } catch (err) {
    console.error("failed to write token.json", err);
  }

  const mintTx = await token.mint(wallet0.address, ethers.parseEther("1000"));
  await mintTx.wait();

  const baseNonce = await provider.getTransactionCount(wallet0.address, "pending");

  const tx1 = await wallet0.sendTransaction({
    to: wallet1.address,
    value: 1n,
    nonce: baseNonce + 1,
    gasLimit: 21000n,
    gasPrice: 1_000_000_000n
  });

  const tx2 = await wallet0.sendTransaction({
    to: wallet1.address,
    value: 2n,
    nonce: baseNonce + 2,
    gasLimit: 21000n,
    gasPrice: 1_000_000_000n
  });

  const tx3 = await token.transfer(wallet1.address, 100n, { nonce: baseNonce + 3 });

  console.log("pending txs", tx1.hash, tx2.hash, tx3.hash);

  await sleep(2000);
  await network.provider.send("evm_mine");
  console.log("mined empty block");

  const replacement = await wallet0.sendTransaction({
    to: wallet1.address,
    value: 3n,
    nonce: baseNonce + 2,
    gasLimit: 21000n,
    gasPrice: 2_000_000_000n
  });
  console.log("replacement tx", replacement.hash);

  const gapFill = await wallet0.sendTransaction({
    to: wallet1.address,
    value: 4n,
    nonce: baseNonce,
    gasLimit: 21000n,
    gasPrice: 1_000_000_000n
  });
  console.log("gap fill tx", gapFill.hash);

  await network.provider.send("evm_mine");
  await network.provider.send("evm_mine");

  console.log("done");
}

function sleep(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

main().catch(err => {
  console.error(err);
  process.exit(1);
});
