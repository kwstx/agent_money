export class AgentMoneyError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'AgentMoneyError';
  }
}

export class PolicyViolationError extends AgentMoneyError {
  public policyDetails: any;
  constructor(message: string, policyDetails?: any) {
    super(message);
    this.name = 'PolicyViolationError';
    this.policyDetails = policyDetails;
  }
}

export class RailFailureError extends AgentMoneyError {
  public transactionId?: string;
  constructor(message: string, transactionId?: string) {
    super(message);
    this.name = 'RailFailureError';
    this.transactionId = transactionId;
  }
}

export class AuthenticationError extends AgentMoneyError {
  constructor(message: string) {
    super(message);
    this.name = 'AuthenticationError';
  }
}
