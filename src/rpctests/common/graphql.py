""" GraphQL utilities """

import gql
import gql.transport.aiohttp
import gql.transport.exceptions
import graphql.execution.execute
import logging


# Configure logging
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')
logger = logging.getLogger(__name__)


class QueryProcessor:
    """ GraphQL query processor """
    def __init__(self, node_url: str):
        """ Initialize the GraphQL query processor.
            node_url: WebSocket URL of the Ethereum node
        """
        self.node_url = node_url
        self.transport = gql.transport.aiohttp.AIOHTTPTransport(url=node_url)
        self.client = gql.Client(transport=self.transport, parse_results=False)
        self.session = None

    async def close(self):
        await self.client.close_async()

    async def __aenter__(self):
        return QuerySession(await self.client.connect_async())

    async def __aexit__(self, exc_type, exc, tb):
        await self.client.close_async()

    async def execute_async(self, query: str) -> graphql.execution.ExecutionResult:
        """Execute query towards the Ethereum node using an ephemeral session."""
        gql_query = gql.gql(query)
        try:
            return await self.client.execute_async(request=gql_query, get_execution_result=True)
        except gql.transport.exceptions.TransportQueryError as e:
            return graphql.execution.ExecutionResult(e.data, e.errors, e.extensions)


class QuerySession:
    """ GraphQL query session """
    def __init__(self, session: gql.client.AsyncClientSession):
        """ Initialize the GraphQL query session.
            session: the async GraphQL session
        """
        self.session = session

    async def execute(self, query: str) -> graphql.execution.ExecutionResult:
        """Execute query towards the Ethereum node."""
        gql_query = gql.gql(query)
        try:
            return await self.session.execute(request=gql_query, get_execution_result=True)
        except gql.transport.exceptions.TransportQueryError as e:
            return graphql.execution.ExecutionResult(e.data, e.errors, e.extensions)
