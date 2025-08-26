SET CLUSTER SETTING kv.rangefeed.enabled = true;

CREATE CHANGEFEED FOR TABLE public.warehouse
INTO 'webhook-https://host.docker.internal:8443/cdc/w_id?insecure_tls_skip_verify=true'
WITH updated;

CREATE CHANGEFEED FOR TABLE public.district
INTO 'webhook-https://host.docker.internal:8443/cdc/d_w_id,d_id?insecure_tls_skip_verify=true'
WITH updated;

CREATE CHANGEFEED FOR TABLE public.customer
INTO 'webhook-https://host.docker.internal:8443/cdc/c_w_id,c_d_id,c_id?insecure_tls_skip_verify=true'
WITH updated;

CREATE CHANGEFEED FOR TABLE public.history
INTO 'webhook-https://host.docker.internal:8443/cdc/h_w_id,rowid?insecure_tls_skip_verify=true'
WITH updated;

CREATE CHANGEFEED FOR TABLE public."order"
INTO 'webhook-https://host.docker.internal:8443/cdc/o_w_id,o_d_id,o_id?insecure_tls_skip_verify=true'
WITH updated;

CREATE CHANGEFEED FOR TABLE public.new_order
INTO 'webhook-https://host.docker.internal:8443/cdc/no_w_id,no_d_id,no_o_id?insecure_tls_skip_verify=true'
WITH updated;

CREATE CHANGEFEED FOR TABLE public.item
INTO 'webhook-https://host.docker.internal:8443/cdc/i_id?insecure_tls_skip_verify=true'
WITH updated;

CREATE CHANGEFEED FOR TABLE public.stock
INTO 'webhook-https://host.docker.internal:8443/cdc/s_w_id,s_i_id?insecure_tls_skip_verify=true'
WITH updated;

CREATE CHANGEFEED FOR TABLE public.order_line
INTO 'webhook-https://host.docker.internal:8443/cdc/ol_w_id,ol_d_id,ol_o_id,ol_number?insecure_tls_skip_verify=true'
WITH updated;

