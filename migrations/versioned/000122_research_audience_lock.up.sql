-- Serialize final audience validation + native sharing with all native
-- organization membership mutations. Shares retain their native dynamic
-- membership semantics after the authorization transaction completes.
CREATE OR REPLACE FUNCTION research_lock_organization_audience() RETURNS trigger AS $$
BEGIN
 IF TG_OP = 'UPDATE' AND OLD.organization_id <> NEW.organization_id THEN
  -- A membership moved between organizations must acquire both locks in the
  -- same order, regardless of the direction of that move.
  IF OLD.organization_id < NEW.organization_id THEN
   PERFORM pg_advisory_xact_lock(hashtextextended('research-audience:' || OLD.organization_id, 0));
   PERFORM pg_advisory_xact_lock(hashtextextended('research-audience:' || NEW.organization_id, 0));
  ELSE
   PERFORM pg_advisory_xact_lock(hashtextextended('research-audience:' || NEW.organization_id, 0));
   PERFORM pg_advisory_xact_lock(hashtextextended('research-audience:' || OLD.organization_id, 0));
  END IF;
 ELSIF TG_OP = 'DELETE' THEN
  PERFORM pg_advisory_xact_lock(hashtextextended('research-audience:' || OLD.organization_id, 0));
 ELSE
  PERFORM pg_advisory_xact_lock(hashtextextended('research-audience:' || NEW.organization_id, 0));
 END IF;
 IF TG_OP = 'DELETE' THEN
  RETURN OLD;
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER research_organization_audience_lock
 BEFORE INSERT OR UPDATE OR DELETE ON organization_tenant_members
 FOR EACH ROW EXECUTE FUNCTION research_lock_organization_audience();
