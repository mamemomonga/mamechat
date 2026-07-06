import React from "react";
import ReactDOM from "react-dom/client";
import { createBrowserRouter, RouterProvider } from "react-router";

import AdminApp from "./AdminApp";
import AdminPage from "./routes/AdminPage";
import AdminLoginPage from "./routes/AdminLoginPage";
import "./styles.css";

const router = createBrowserRouter(
  [
    {
      path: "/",
      element: <AdminApp />,
      children: [
        { index: true, element: <AdminPage /> },
        { path: "login", element: <AdminLoginPage /> },
      ],
    },
  ],
  { basename: "/admin" },
);

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <RouterProvider router={router} />
  </React.StrictMode>,
);
