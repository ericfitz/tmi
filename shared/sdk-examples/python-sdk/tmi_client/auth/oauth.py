"""
OAuth authentication handler for TMI API.
"""
import webbrowser
from typing import Dict, Any, Optional, TYPE_CHECKING
from urllib.parse import urlencode, parse_qs, urlparse
import requests

if TYPE_CHECKING:
    from ..client import TMIClient


class OAuthHandler:
    """Handles OAuth authentication flow with TMI API."""
    
    def __init__(self, client: 'TMIClient'):
        """
        Initialize OAuth handler.
        
        Args:
            client: TMI client instance
        """
        self.client = client
    
    def get_available_providers(self) -> Dict[str, Any]:
        """Get list of available OAuth providers."""
        response = requests.get(f'{self.client.base_url}/auth/providers')
        response.raise_for_status()
        return response.json()
    
    def start_oauth_flow(self, provider: str, redirect_uri: Optional[str] = None, 
                        state: Optional[str] = None, open_browser: bool = True) -> str:
        """
        Start OAuth flow by getting the authorization URL.
        
        Args:
            provider: OAuth provider name (e.g., 'google', 'github')
            redirect_uri: Optional redirect URI
            state: Optional state parameter for security
            open_browser: Whether to automatically open browser
            
        Returns:
            Authorization URL for the OAuth flow
        """
        params = {}
        if redirect_uri:
            params['redirect_uri'] = redirect_uri
        if state:
            params['state'] = state
            
        auth_url = f'{self.client.base_url}/auth/login/{provider}'
        if params:
            auth_url += '?' + urlencode(params)
        
        if open_browser:
            webbrowser.open(auth_url)
            
        return auth_url
    
    def exchange_code_for_token(self, provider: str, code: str, 
                               redirect_uri: Optional[str] = None, 
                               state: Optional[str] = None) -> Dict[str, Any]:
        """
        Exchange authorization code for access token.
        
        Args:
            provider: OAuth provider name
            code: Authorization code from OAuth callback
            redirect_uri: Redirect URI used in the flow
            state: State parameter from OAuth callback
            
        Returns:
            Token response containing access_token, refresh_token, etc.
        """
        data = {'code': code}
        if redirect_uri:
            data['redirect_uri'] = redirect_uri
        if state:
            data['state'] = state
        
        response = requests.post(f'{self.client.base_url}/auth/token/{provider}', json=data)
        response.raise_for_status()
        
        token_data = response.json()
        
        # Automatically set the token in the client
        if 'access_token' in token_data:
            self.client.set_token(token_data['access_token'])
        
        return token_data
    
    def handle_callback(self, callback_url: str) -> Dict[str, Any]:
        """
        Handle OAuth callback URL and extract token information.
        
        Args:
            callback_url: The full callback URL with query parameters
            
        Returns:
            Token data if successful
        """
        parsed = urlparse(callback_url)
        query_params = parse_qs(parsed.query)
        
        if 'error' in query_params:
            raise Exception(f"OAuth error: {query_params['error'][0]}")
        
        if 'code' not in query_params:
            raise Exception("No authorization code found in callback URL")
        
        code = query_params['code'][0]
        state = query_params.get('state', [None])[0]
        
        # Extract provider from callback path or use default
        # This might need to be adapted based on your OAuth flow
        provider = "default"  # You might need to store this during the flow
        
        return self.exchange_code_for_token(provider, code, state=state)
    
    def login_with_credentials(self, provider: str, username: str, password: str) -> Dict[str, Any]:
        """
        Login directly with username/password (for test provider or similar).
        
        Args:
            provider: Provider name
            username: Username/email
            password: Password
            
        Returns:
            Token data
        """
        data = {
            'username': username,
            'password': password
        }
        
        response = requests.post(f'{self.client.base_url}/auth/token/{provider}', json=data)
        response.raise_for_status()
        
        token_data = response.json()
        
        # Automatically set the token in the client
        if 'access_token' in token_data:
            self.client.set_token(token_data['access_token'])
        
        return token_data