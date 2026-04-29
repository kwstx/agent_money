import axios from 'axios';

export enum RailType {
  LIGHTNING = 'lightning',
  STRIPE = 'stripe',
  STABLECOIN = 'stablecoin',
  X402 = 'x402',
}

export interface Money {
  amount: string;
  currency: string;
}

export interface TransactionRequest {
  amount: Money;
  rail_type: RailType;
  context?: Record<string, any>;
  constraints?: any[];
  metadata?: Record<string, any>;
}

export interface TransactionResponse extends TransactionRequest {
  transaction_id: string;
  status: 'PENDING' | 'APPROVED' | 'EXECUTED' | 'SETTLED' | 'FAILED';
  created_at: number;
}

export class AgentMoney {
  private baseUrl: string;

  constructor(config: { baseUrl: string }) {
    this.baseUrl = config.baseUrl;
  }

  /**
   * Initiates a new autonomous transaction.
   */
  async spend(request: TransactionRequest): Promise<TransactionResponse> {
    const response = await axios.post(`${this.baseUrl}/transactions`, {
        amount: request.amount.amount,
        currency: request.amount.currency,
        rail_type: request.rail_type,
        context: request.context || {},
        constraints: request.constraints || [],
        metadata: request.metadata || {}
    });
    return response.data;
  }

  /**
   * Retrieves the status of a transaction.
   */
  async getStatus(transactionId: string): Promise<TransactionResponse> {
    const response = await axios.get(`${this.baseUrl}/transactions/${transactionId}`);
    return response.data;
  }
}
