import { describe, expect, it } from "vitest";
import { ARCHIVE_MISSING_INVOICE, ARCHIVE_MISSING_RECEIPT, isBooked, submitOutcome } from "./submitOutcome";

const PAYMENT = "De factuur is geboekt, maar de betaling niet.";
const ARCHIVE = "De boeking is gelukt, maar het document staat nog in de mailmap.";

describe("submitOutcome", () => {
  it("is ok without warnings", () => {
    expect(submitOutcome({ isReceipt: false, archiveExpected: true, resp: { archived: true } })).toEqual({
      status: "ok",
      warnings: [],
    });
  });

  it("reports a payment warning alone", () => {
    expect(
      submitOutcome({ isReceipt: false, archiveExpected: false, resp: { archived: false, paymentWarning: PAYMENT } }),
    ).toEqual({ status: "warning", warnings: [PAYMENT], outcome: "payment_warning" });
  });

  it("reports an archive warning alone", () => {
    expect(
      submitOutcome({ isReceipt: false, archiveExpected: false, resp: { archived: true, archiveWarning: ARCHIVE } }),
    ).toEqual({ status: "warning", warnings: [ARCHIVE], outcome: "archive_warning" });
  });

  // The bug: a payment warning hid the archive warning of the same booking.
  it("reports a payment and an archive warning together, the payment first", () => {
    expect(
      submitOutcome({
        isReceipt: false,
        archiveExpected: false,
        resp: { archived: true, paymentWarning: PAYMENT, archiveWarning: ARCHIVE },
      }),
    ).toEqual({ status: "warning", warnings: [PAYMENT, ARCHIVE], outcome: "payment_and_archive_warning" });
  });

  it("reports a payment warning and an upload that did not reach the archive", () => {
    expect(
      submitOutcome({ isReceipt: false, archiveExpected: true, resp: { archived: false, paymentWarning: PAYMENT } }),
    ).toEqual({
      status: "warning",
      warnings: [PAYMENT, ARCHIVE_MISSING_INVOICE],
      outcome: "payment_and_archive_warning",
    });
  });

  it("names the document kind when an expected archive is missing", () => {
    expect(submitOutcome({ isReceipt: true, archiveExpected: true, resp: { archived: false } }).warnings).toEqual([
      ARCHIVE_MISSING_RECEIPT,
    ]);
    expect(submitOutcome({ isReceipt: false, archiveExpected: true, resp: { archived: false } }).warnings).toEqual([
      ARCHIVE_MISSING_INVOICE,
    ]);
  });

  it("prefers the backend's archive warning over the generic one", () => {
    expect(
      submitOutcome({ isReceipt: false, archiveExpected: true, resp: { archived: false, archiveWarning: ARCHIVE } })
        .warnings,
    ).toEqual([ARCHIVE]);
  });

  it("does not warn when no archive was expected", () => {
    // A mail-folder document or an upload without a folder: archived false is normal.
    expect(submitOutcome({ isReceipt: false, archiveExpected: false, resp: { archived: false } }).status).toBe("ok");
    // An older answer without the field is not a failure either.
    expect(submitOutcome({ isReceipt: true, archiveExpected: true, resp: {} }).status).toBe("ok");
  });

  it("ignores blank warnings", () => {
    expect(
      submitOutcome({ isReceipt: false, archiveExpected: false, resp: { paymentWarning: " ", archiveWarning: "" } }),
    ).toEqual({ status: "ok", warnings: [] });
  });
});

describe("isBooked", () => {
  it("counts a booking with a warning as booked", () => {
    expect(isBooked("ok")).toBe(true);
    expect(isBooked("warning")).toBe(true);
    expect(isBooked("error")).toBe(false);
    expect(isBooked("skipped")).toBe(false);
  });
});
