import { AgentClient, PolicyViolationError } from '../../sdks/typescript/src';

async function runAutonomousWorkflow() {
  const client = new AgentClient({
    baseUrl: 'http://localhost:8080',
    apiKey: 'your_api_key',
    agentId: 'writer_agent_42'
  });

  const task = "Generate high-resolution images for marketing campaign";
  const estimatedCost = 1.25;

  console.log(`Starting autonomous task: ${task}`);
  console.log(`Estimated cost: $${estimatedCost}`);

  try {
    const result = await client.spend({
      amount: estimatedCost,
      context: {
        action: 'image_generation',
        service: 'dalle-3',
        priority: 'premium'
      }
    });

    console.log(`Authorization success! TxID: ${result.transaction_id}`);
    console.log(`Selected Rail: ${result.rail}`);

    // Proceed with image generation...
    console.log("Generating images...");

  } catch (error) {
    if (error instanceof PolicyViolationError) {
      console.warn(`Action restricted by policy: ${error.message}`);
      console.info("Agent Action: Adjusting parameters to reduce cost.");
    } else {
      console.error("Workflow halted due to error:", error);
    }
  }
}

runAutonomousWorkflow();
