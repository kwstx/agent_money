export interface TransactionResult {
  transaction_id: string;
  status: string;
  rail?: string;
  estimated_cost: string;
  raw_response: any;
}

export interface SpendRequest {
  request_id?: string;
  amount: number;
  currency?: string;
  context: Record<string, any>;
  constraints?: any[];
}

export interface ClientConfig {
  baseUrl: string;
  apiKey: string;
  agentId: string;
  timeout?: number;
  maxRetries?: number;
}
