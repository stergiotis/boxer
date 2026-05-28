/*Query 1: Extracting Cyber Threats and their Nested Ports (The "How-To")
Let's decode the CyberShield data. We want to list the attack types (Symbol) and the target network ports (lr low-cardinality references). We will use the UDF to map the ports to the correct attack attribute, and ARRAY JOIN to explode them into relational rows.
 */
WITH ANCHOR_UNFLATTEN_LEEWAY_ARRAY(
    `tv:symbol:lr:lr:u64:2q:0:0:0::data`,
    `tv:symbol:lrcard:lrcard:u64:4gw:0:0:0::data`
) AS nested_target_ports
SELECT
    `id:id:u64:2k:0:0:` AS id,
    `id:naturalKey:y:g:0:0:` AS incident_ticket,
    attack_type,
    target_ports
FROM anchor.facts
-- Parallel ARRAY JOIN explodes the lists so each attribute becomes a row
ARRAY JOIN
    `tv:symbol:value:val:s:m:0:24:0::data` AS attack_type,
    nested_target_ports AS target_ports
WHERE has(['DDOS', 'SQL_INJECTION', 'PORT_SCAN'], attack_type);
