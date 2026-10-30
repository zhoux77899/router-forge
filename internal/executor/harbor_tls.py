"""Explicit TLS policy for one owned Harbor or container Python process."""

import functools
import importlib
import inspect
import os
import ssl


class UnsupportedTLSPolicy(Exception):
    pass


def apply_tls_policy(skip, ca=None, policy_for_phase=None):
    if type(skip) is not bool:
        raise UnsupportedTLSPolicy("TLS policy must be a boolean")
    active = getattr(ssl, "_router_forge_policy_active", False)
    if active:
        if not skip and getattr(ssl, "_router_forge_skip_tls", False) and policy_for_phase is None:
            raise UnsupportedTLSPolicy("strict TLS requires a fresh process")
        return
    if not skip and not ca and policy_for_phase is None:
        return

    default_context = ssl.create_default_context

    def selected_policy():
        selected = policy_for_phase() if policy_for_phase is not None else (skip, ca)
        if not isinstance(selected, tuple) or len(selected) != 2 or type(selected[0]) is not bool:
            raise UnsupportedTLSPolicy("invalid scoped TLS policy")
        return selected

    @functools.wraps(default_context)
    def scoped_context(*args, **kwargs):
        selected_skip, selected_ca = selected_policy()
        # Use compiled OS trust, never CA environment inherited from another
        # phase. A custom bundle augments rather than replaces that trust.
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
        paths = ssl.get_default_verify_paths()
        cafile = paths.openssl_cafile if paths.openssl_cafile and os.path.isfile(paths.openssl_cafile) else None
        capath = paths.openssl_capath if paths.openssl_capath and os.path.isdir(paths.openssl_capath) else None
        if not selected_skip:
            if os.name == "nt":
                for encoding in ("ROOT", "CA"):
                    for certificate, kind, trust in ssl.enum_certificates(encoding):
                        if kind == "x509_asn" and (trust is True or ssl.Purpose.SERVER_AUTH.oid in trust):
                            context.load_verify_locations(cadata=certificate)
            else:
                if not cafile and not capath:
                    raise UnsupportedTLSPolicy("native system certificate trust is unavailable")
                context.load_verify_locations(cafile=cafile, capath=capath)
        context.minimum_version = ssl.TLSVersion.TLSv1_2
        if selected_skip:
            context.check_hostname = False
            context.verify_mode = ssl.CERT_NONE
        elif selected_ca:
            context.load_verify_locations(cafile=selected_ca)
        return context

    ssl.create_default_context = scoped_context
    ssl._create_default_https_context = scoped_context
    ssl._router_forge_scoped_context = scoped_context
    try:
        httpx = importlib.import_module("httpx")
    except ModuleNotFoundError as exc:
        if exc.name != "httpx":
            raise UnsupportedTLSPolicy("HTTPX dependencies are unavailable") from None
    else:
        for client in (httpx.Client, httpx.AsyncClient):
            original = client.__init__
            if "verify" not in inspect.signature(original).parameters:
                raise UnsupportedTLSPolicy("HTTPX lacks an explicit TLS verification option")

            def wrap_constructor(constructor):
                @functools.wraps(constructor)
                def init(self, *args, **kwargs):
                    kwargs["verify"] = scoped_context()
                    return constructor(self, *args, **kwargs)
                return init

            client.__init__ = wrap_constructor(original)

    for name in ("requests", "pip._vendor.requests"):
        try:
            requests = importlib.import_module(name)
        except ModuleNotFoundError as exc:
            if exc.name == name or name.startswith(exc.name + "."):
                continue
            raise UnsupportedTLSPolicy("Requests dependencies are unavailable") from None
        original = requests.Session.send
        if not any(parameter.kind == inspect.Parameter.VAR_KEYWORD for parameter in inspect.signature(original).parameters.values()):
            raise UnsupportedTLSPolicy("Requests lacks an explicit TLS verification option")

        if policy_for_phase is not None or ca is not None:
            try:
                adapter_type = importlib.import_module(name + ".adapters").HTTPAdapter
                if not all(callable(getattr(adapter_type, method, None)) for method in ("send", "init_poolmanager", "cert_verify", "proxy_manager_for")):
                    raise AttributeError()
            except (ImportError, AttributeError):
                raise UnsupportedTLSPolicy("Requests lacks scoped TLS adapter APIs") from None

            def wrap_scoped_send(send, base_adapter):
                @functools.wraps(send)
                def request(self, *args, **kwargs):
                    from copy import copy
                    from urllib.parse import urlsplit
                    context = scoped_context()

                    class ScopedAdapter(base_adapter):
                        def init_poolmanager(self, connections, maxsize, block=False, **options):
                            options["ssl_context"] = context
                            return super().init_poolmanager(connections, maxsize, block=block, **options)

                        def proxy_manager_for(self, proxy, **options):
                            options.update(ssl_context=context, proxy_ssl_context=context)
                            return super().proxy_manager_for(proxy, **options)

                        def cert_verify(self, connection, url, verify, cert):
                            if cert is not None:
                                raise UnsupportedTLSPolicy("client certificate overrides are unsupported")

                        def build_connection_pool_key_attributes(self, request, verify, cert=None):
                            if cert is not None:
                                raise UnsupportedTLSPolicy("client certificate overrides are unsupported")
                            url = urlsplit(request.url)
                            return {"scheme":url.scheme,"host":url.hostname,"port":url.port}, {"ssl_context":context,"cert_reqs":"CERT_NONE" if context.verify_mode == ssl.CERT_NONE else "CERT_REQUIRED"}

                    # A reused source Session cannot lend its pooled TLS context
                    # to verifier work. Cookies/hooks stay native; adapters do not.
                    session = copy(self)
                    adapter = ScopedAdapter(max_retries=0)
                    session.adapters = {"https://":adapter,"http://":adapter}
                    session.trust_env = False
                    kwargs["verify"] = True
                    try:
                        return send(session, *args, **kwargs)
                    finally:
                        adapter.close()
                return request

            requests.Session.send = wrap_scoped_send(original,adapter_type)
        else:
            def wrap_send(send):
                @functools.wraps(send)
                def request(self, *args, **kwargs):
                    selected_skip, selected_ca = selected_policy()
                    kwargs["verify"] = False if selected_skip else selected_ca or True
                    return send(self, *args, **kwargs)
                return request

            requests.Session.send = wrap_send(original)
    ssl._router_forge_skip_tls = skip
    ssl._router_forge_policy_active = True
