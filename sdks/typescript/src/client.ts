import axios, { AxiosInstance } from 'axios';
import { v4 as uuidv4 } from 'uuid';
import { 
  TransactionResult, 
  SpendRequest, 
  ClientConfig 
} from './types';
import { 
  AgentMoneyError, 
  PolicyViolationError, 
  RailFailureError, 
  AuthenticationError 
} from './errors';

export class AgentClient {
  private axiosInstance: AxiosInstance;
  private config: ClientConfig;

  constructor(config: ClientConfig) {
    this.config = {
      timeout: 30000,
      maxRetries: 3,
      ...config
    };

    this.axiosInstance = axios.create({
      baseURL: this.config.baseUrl.replace(/\/$/, ''),
      timeout: this.config.timeout,
      headers: {
        'Authorization': `Bearer ${this.config.apiKey}`,
        'X-Consumer-Username': this.config.agentId,
        'Content-Type': 'application/json'
      }
    });
  }

  async spend(request: SpendRequest): Promise<TransactionResult> {
    const requestId = request.request_id || uuidv4();
    const payload = {
      request_id: requestId,
      amount: request.amount,
      currency: request.currency || 'USD',
      context: request.context,
      constraints: request.constraints || []
    };

    let retries = 0;
    const maxRetries = this.config.maxRetries || 3;

    while (retries <= maxRetries) {
      try {
        const response = await this.axiosInstance.post('/spend', payload);
        
        return {
          transaction_id: response.data.transaction_id,
          status: response.data.status,
          rail: response.data.rail,
          estimated_cost: response.data.estimated_cost,
          raw_response: response.data
        };

      } catch (error: any) {
        if (axios.isAxiosError(error)) {
          const status = error.response?.status;
          const data = error.response?.data;

          if (status === 401) {
            throw new AuthenticationError('Invalid API key');
          }
          if (status === 403) {
            throw new PolicyViolationError('Policy violation: ' + (data?.message || JSON.stringify(data)), data);
          }
          if (status === 429) {
            throw new AgentMoneyError('Rate limit exceeded');
          }
          if (status && status >= 500) {
            if (retries < maxRetries) {
              retries++;
              await new Promise(resolve => setTimeout(resolve, Math.pow(2, retries) * 1000));
              continue;
            }
            throw new RailFailureError('Server error: ' + (data?.message || error.message));
          }
        }

        if (retries < maxRetries) {
          retries++;
          await new Promise(resolve => setTimeout(resolve, Math.pow(2, retries) * 1000));
          continue;
        }
        throw new AgentMoneyError('Network error: ' + error.message);
      }
    }

    throw new AgentMoneyError('Max retries exceeded');
  }
}
