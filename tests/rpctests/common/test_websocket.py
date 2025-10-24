""" Tests for WebSocket utilities """

import eth_typing
import pytest
import unittest.mock as mock
import web3
import web3.eth

from rpctests.common.websocket import Client


# Mock the entire web3 module to control connection behavior
@pytest.fixture
def mock_web3():
    """Mocks the web3 and WebSocketProvider classes."""
    with mock.patch('rpctests.common.websocket.web3') as mock_w3_module:
        # Mock the Provider and its methods
        mock_provider = mock.AsyncMock()
        mock_provider.connect = mock.AsyncMock()
        mock_provider.disconnect = mock.AsyncMock()

        # Mock the AsyncWeb3 object
        mock_async_w3 = mock.MagicMock()
        mock_async_w3.provider = mock_provider

        # Mock eth methods (e.g., block_number)
        mock_async_w3.eth.block_number = mock.AsyncMock(return_value=12345)

        # Mock is_connected to control connection success/failure
        mock_async_w3.is_connected = mock.AsyncMock(return_value=True)

        # Set the mocked objects on the web3 module mock
        mock_w3_module.WebSocketProvider.return_value = mock_provider
        mock_w3_module.AsyncWeb3.return_value = mock_async_w3

        yield mock_w3_module


# Mock the ssl module for WSS tests
@pytest.fixture
def mock_ssl():
    with mock.patch('rpctests.common.websocket.ssl') as mock_ssl_module:
        # Mock the necessary methods and objects
        mock_ssl_context = mock.MagicMock()
        mock_ssl_module.SSLContext.return_value = mock_ssl_context
        yield mock_ssl_module


# Mock urllib.parse to control URL parsing
@pytest.fixture
def mock_urlparse():
    with mock.patch('rpctests.common.websocket.urllib.parse.urlparse') as mock_parse:
        yield mock_parse


# Mock async function to control web3.eth.block_number property
async def mock_latest_block_number() -> eth_typing.BlockNumber:
    """Returns the mocked value when awaited."""
    return eth_typing.BlockNumber(12345)


@pytest.mark.asyncio
@mock.patch('rpctests.common.websocket.web3.WebSocketProvider')
@mock.patch('rpctests.common.websocket.web3.AsyncWeb3')
@mock.patch('rpctests.common.websocket.urllib.parse.urlparse')
async def test_connect_ws_success(mock_urlparse, mock_async_w3_cls, mock_provider_cls):
    """Test successful connection for a non-secure WebSocket (ws)."""

    # Set return values on the mocks
    mock_urlparse.return_value = mock.MagicMock(scheme='ws', netloc='localhost')
    mock_provider = mock_provider_cls.return_value
    mock_provider.connect = mock.AsyncMock()
    mock_provider.disconnect = mock.AsyncMock()
    mock.patch.object(web3.eth.async_eth.AsyncEth, 'block_number', new_callable=mock_latest_block_number)
    mock_async_w3_instance = mock_async_w3_cls.return_value
    mock_async_w3_instance.is_connected = mock.AsyncMock(return_value=True)
    mock_async_w3_instance.eth = web3.eth.async_eth.AsyncEth(mock_async_w3_instance)

    # Execute the test
    client = Client(node_url='ws://localhost:8546')
    await client.connect()

    # Assertions
    assert mock_async_w3_instance.is_connected.called


@pytest.mark.asyncio
@mock.patch('rpctests.common.websocket.ssl')
@mock.patch('rpctests.common.websocket.web3.WebSocketProvider')
@mock.patch('rpctests.common.websocket.web3.AsyncWeb3')
@mock.patch('rpctests.common.websocket.urllib.parse.urlparse')
async def test_connect_wss_success(mock_urlparse, mock_async_w3_cls, mock_provider_cls, mock_ssl):
    """Test successful connection for a secure WebSocket (wss)."""

    # Set return values on the mocks
    mock_urlparse.return_value = mock.MagicMock(scheme='wss', netloc='localhost')
    mock_provider = mock_provider_cls.return_value
    mock_provider.connect = mock.AsyncMock()
    mock_provider.disconnect = mock.AsyncMock()
    mock.patch.object(web3.eth.async_eth.AsyncEth, 'block_number', new_callable=mock_latest_block_number)
    mock_async_w3_instance = mock_async_w3_cls.return_value
    mock_async_w3_instance.is_connected = mock.AsyncMock(return_value=True)
    mock_async_w3_instance.eth = web3.eth.async_eth.AsyncEth(mock_async_w3_instance)

    # Execute the test
    client = Client(node_url='wss://localhost:8547', server_ca_file='/path/to/ca.crt')
    await client.connect()

    # Assertions
    assert mock_async_w3_instance.is_connected.called
    mock_ssl.SSLContext.assert_called_once_with(mock_ssl.PROTOCOL_TLS_CLIENT)


@pytest.mark.asyncio
async def test_connect_wss_missing_ca_file(mock_urlparse):
    """Test connection failure when WSS is used but server_ca_file is None."""
    mock_urlparse.return_value = mock.MagicMock(scheme='wss', netloc='localhost')

    client = Client(node_url='wss://localhost:8547')

    with pytest.raises(ConnectionError) as exc_info:
        await client.connect()

    assert 'non-empty server CA file' in str(exc_info.value)


@pytest.mark.asyncio
async def test_connect_invalid_scheme(mock_urlparse):
    """Test connection failure when URL scheme is neither ws nor wss."""
    mock_urlparse.return_value = mock.MagicMock(scheme='http', netloc='localhost')

    client = Client(node_url='http://localhost')

    with pytest.raises(ConnectionError) as exc_info:
        await client.connect()

    assert 'Invalid WebSocket URL scheme' in str(exc_info.value)


@pytest.mark.asyncio
async def test_connect_failure(mock_web3, mock_urlparse):
    """Test failure when w3.is_connected() returns False."""
    mock_urlparse.return_value = mock.MagicMock(scheme='ws', netloc='localhost')

    # Set the mock to return False for connection status
    mock_async_w3 = mock_web3.AsyncWeb3.return_value
    mock_async_w3.is_connected = mock.AsyncMock(return_value=False)

    client = Client(node_url='ws://localhost:8546')

    with pytest.raises(ConnectionError) as exc_info:
        await client.connect()

    assert 'Failed to connect to Ethereum node' in str(exc_info.value)


@pytest.mark.asyncio
async def test_subscribe(mock_web3):
    """Test the subscribe method calls the underlying web3 manager."""
    mock_async_w3 = mock_web3.AsyncWeb3.return_value
    mock_sub_manager = mock.AsyncMock()
    mock_async_w3.subscription_manager = mock_sub_manager

    client = Client(node_url='ws://mock')
    client.w3 = mock_async_w3  # Manually set w3 object

    subscriptions = ['newHeads', 'logs']
    await client.subscribe(subscriptions)

    mock_sub_manager.subscribe.assert_called_once_with(subscriptions)


@pytest.mark.asyncio
async def test_unsubscribe(mock_web3):
    """Test the unsubscribe method calls the underlying web3 manager."""
    mock_async_w3 = mock_web3.AsyncWeb3.return_value
    mock_sub_manager = mock.AsyncMock()
    mock_sub_manager.subscriptions = [1, 2, 3]
    mock_async_w3.subscription_manager = mock_sub_manager

    client = Client(node_url='ws://mock')
    client.w3 = mock_async_w3

    await client.unsubscribe()

    mock_sub_manager.unsubscribe.assert_called_once_with([1, 2, 3])


@pytest.mark.asyncio
async def test_handle_subscriptions(mock_web3):
    """Test the handle_subscriptions method."""
    mock_async_w3 = mock_web3.AsyncWeb3.return_value
    mock_sub_manager = mock.AsyncMock()
    mock_async_w3.subscription_manager = mock_sub_manager

    client = Client(node_url='ws://mock')
    client.w3 = mock_async_w3

    await client.handle_subscriptions(run_forever=True)

    mock_sub_manager.handle_subscriptions.assert_called_once_with(True)


@pytest.mark.asyncio
async def test_disconnect_success(mock_web3):
    """Test successful disconnection."""
    mock_provider = mock_web3.WebSocketProvider.return_value

    # Simulate a successful connection setting up client.w3
    client = Client(node_url='ws://mock')
    client.w3 = mock_web3.AsyncWeb3.return_value
    client.w3.provider = mock_provider

    await client.disconnect()

    mock_provider.disconnect.assert_called_once()


@pytest.mark.asyncio
async def test_disconnect_already_closed():
    """Test disconnect when client.w3 is None."""
    client = Client(node_url='ws://mock')
    client.w3 = None

    # Should run without error
    await client.disconnect()
